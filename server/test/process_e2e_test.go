package e2e_test

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	itemv1 "finga/api/gen/item/v1"
	"finga/pkg/grpcx"
)

// process is a binary in its own process, so it can be killed and restarted on the same addresses.
type process struct {
	t         *testing.T
	name      string
	bin       string
	env       []string
	listeners map[string]string // listener name → the setting of its address
	addrs     map[string]string // taken on the first start, kept for restarts
	url       string            // the gateway's base URL
	grpc      string            // a service's gRPC address
	cmd       *exec.Cmd
	out       lockedBuffer // written by exec's copier goroutine while the test reads it
	done      chan struct{}
}

// listenersOf is the named binary's listeners, each with the setting of its address: the module's own section, or the shared http for the gateway's players.
func listenersOf(name string) map[string]string {
	module := "FINGA_" + strings.ToUpper(name)
	if name == "gateway" {
		return map[string]string{"http": "FINGA_HTTP_ADDR", "ops": module + "_OPS_ADDR"}
	}
	return map[string]string{"grpc": module + "_GRPC_LISTEN", "ops": module + "_OPS_ADDR"}
}

// lockedBuffer is a bytes.Buffer safe for one writer and one reader.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// binDir holds the binaries the process tests build, once for the package; TestMain removes it.
var binDir string

var buildOnce = sync.OnceValues(func() ([]byte, error) {
	build := exec.Command("go", "build", "-o", binDir, "./cmd/gateway", "./cmd/item", "./cmd/account", "./cmd/social", "./cmd/character", "./cmd/world")
	build.Dir = ".."
	return build.CombinedOutput()
})

func buildBinaries(t *testing.T) map[string]string {
	t.Helper()
	if out, err := buildOnce(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	bins := map[string]string{}
	for _, name := range []string{"gateway", "item", "account", "social", "character", "world"} {
		bins[name] = filepath.Join(binDir, name)
		if runtime.GOOS == "windows" {
			bins[name] += ".exe"
		}
	}
	return bins
}

// baseEnv points every process at the test database and the game data; info level logs what a failed start shows.
func baseEnv(dbURL, data string) []string {
	return append(os.Environ(), "FINGA_DATABASE_URL="+dbURL, "FINGA_DATA_DIR="+data, "FINGA_LOG_LEVEL=info")
}

// startServiceProcess runs a service binary on free ports; extra names the services it dials.
func startServiceProcess(t *testing.T, name, bin, dbURL, data string, extra ...string) *process {
	t.Helper()
	p := &process{t: t, name: name, bin: bin, env: append(baseEnv(dbURL, data), extra...), listeners: listenersOf(name)}
	p.start()
	p.grpc = p.addrs["grpc"]
	return p
}

// dependency is the override that makes the dialer binary dial name at svc's address.
func dependency(dialer, name string, svc *process) string {
	return "FINGA_" + strings.ToUpper(dialer) + "_DEPENDENCIES_" + strings.ToUpper(name) + "=" + svc.grpc
}

func startGateway(t *testing.T, bin, dbURL, data string, services map[string]*process) *process {
	t.Helper()
	p := &process{t: t, name: "gateway", bin: bin, env: baseEnv(dbURL, data), listeners: listenersOf("gateway")}
	for name, svc := range services {
		p.env = append(p.env, dependency("gateway", name, svc))
	}
	p.start()
	p.url = "http://" + p.addrs["http"]
	return p
}

// start runs the binary until healthy, on the same addresses each time.
func (p *process) start() {
	p.t.Helper()
	names := slices.Sorted(maps.Keys(p.listeners))
	if p.addrs == nil {
		p.addrs = map[string]string{}
		for k, addr := range freeAddrs(p.t, len(names)) {
			p.addrs[names[k]] = addr
		}
	}
	env := slices.Clone(p.env)
	for _, name := range names {
		env = append(env, p.listeners[name]+"="+p.addrs[name])
	}
	p.cmd = exec.Command(p.bin)
	p.cmd.Dir = ".." // server/, where the binaries run
	p.cmd.Env = env
	p.cmd.Stdout = &p.out
	p.cmd.Stderr = &p.out
	if err := p.cmd.Start(); err != nil {
		p.t.Fatal(err)
	}
	p.done = make(chan struct{})
	cmd, done := p.cmd, p.done
	go func() { defer close(done); _ = cmd.Wait() }()
	p.t.Cleanup(p.kill)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if p.healthy() {
			return
		}
		select {
		case <-done:
			p.t.Fatalf("%s exited before it was healthy:\n%s", p.name, p.out.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	p.t.Fatalf("%s not healthy within 20 s:\n%s", p.name, p.out.String())
}

func (p *process) healthy() bool {
	res, err := http.Get("http://" + p.addrs["ops"] + "/healthz")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// kill ends the process without warning: no shutdown, no checkpoint, no flush.
func (p *process) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}

// terminate sends SIGTERM and waits up to d for the process to exit, reporting how long it took.
func (p *process) terminate(d time.Duration) time.Duration {
	p.t.Helper()
	start := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		p.t.Fatal(err)
	}
	select {
	case <-p.done:
		return time.Since(start)
	case <-time.After(d):
		p.kill()
		p.t.Fatalf("%s still running %s after SIGTERM:\n%s", p.name, d, p.out.String())
		return 0
	}
}

// processStack runs every service and the gateway as their own processes, by name, over the game data in data; world adds to the world's settings.
func processStack(t *testing.T, data string, world ...string) (*stack, map[string]*process) {
	t.Helper()
	bins := buildBinaries(t)
	dbURL := cluster.URL(t)
	abs, err := filepath.Abs(data) // the binaries run in server/
	if err != nil {
		t.Fatal(err)
	}
	procs := map[string]*process{}
	for _, name := range []string{"item", "account", "social"} {
		procs[name] = startServiceProcess(t, name, bins[name], dbURL, abs)
	}
	procs["character"] = startServiceProcess(t, "character", bins["character"], dbURL, abs, dependency("character", "item", procs["item"]))
	world = append([]string{dependency("world", "character", procs["character"]), dependency("world", "item", procs["item"]), dependency("world", "social", procs["social"])}, world...)
	procs["world"] = startServiceProcess(t, "world", bins["world"], dbURL, abs, world...)
	gateway := startGateway(t, bins["gateway"], dbURL, abs, maps.Clone(procs))
	procs["gateway"] = gateway
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	itemConn, err := grpcx.Dial(procs["item"].grpc) // the address outlives a restart
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = itemConn.Close() })
	return &stack{t: t, url: gateway.url, db: db, items: itemv1.NewItemServiceClient(itemConn), data: data}, procs
}
