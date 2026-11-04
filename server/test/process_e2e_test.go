package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
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
)

// process is a binary in its own process, so it can be killed and restarted on the same addresses.
type process struct {
	t         *testing.T
	name      string
	bin       string
	env       []string
	listeners map[string]string // listener name → the setting of its address
	healthOn  string            // the listener that serves /healthz
	addrs     map[string]string // bound on the first start, kept for restarts
	url       string            // the gateway's base URL
	grpc      string            // a service's gRPC address
	cmd       *exec.Cmd
	out       lockedBuffer // written by exec's copier goroutine while the test reads it
	done      chan struct{}
}

// The binaries' listeners, each with the setting of its address.
var (
	serviceListeners = map[string]string{"grpc": "FINGA_GRPC_LISTEN", "ops": "FINGA_OPS_ADDR"}
	gatewayListeners = map[string]string{"http": "FINGA_HTTP_ADDR"}
)

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

// listening is the address each listener logged it bound, in the output after from.
func (b *lockedBuffer) listening(from int) map[string]string {
	addrs := map[string]string{}
	for line := range strings.Lines(b.String()[from:]) {
		var e struct{ Msg, Listener, Addr string }
		if json.Unmarshal([]byte(line), &e) == nil && e.Msg == "listening" {
			addrs[e.Listener] = e.Addr
		}
	}
	return addrs
}

func buildBinaries(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", dir, "./services/gateway", "./services/item", "./services/account", "./services/social", "./services/character", "./services/world")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	bins := map[string]string{}
	for _, name := range []string{"gateway", "item", "account", "social", "character", "world"} {
		bins[name] = filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			bins[name] += ".exe"
		}
	}
	return bins
}

// baseEnv points every process at the test database; info level logs the "listening" lines start reads.
func baseEnv(dbURL string) []string {
	return append(os.Environ(), "FINGA_DATABASE_URL="+dbURL, "FINGA_LOG_LEVEL=info")
}

// startServiceProcess runs a service binary on ports it picks; extra names the services it dials.
func startServiceProcess(t *testing.T, name, bin, dbURL string, extra ...string) *process {
	t.Helper()
	p := &process{t: t, name: name, bin: bin, env: append(baseEnv(dbURL), extra...), listeners: serviceListeners, healthOn: "ops"}
	p.start()
	p.grpc = p.addrs["grpc"]
	return p
}

// dependency is the override that makes a binary dial name at svc's address.
func dependency(name string, svc *process) string {
	return "FINGA_DEPENDENCIES_" + strings.ToUpper(name) + "=" + svc.grpc
}

func startGateway(t *testing.T, bin, dbURL string, services map[string]*process) *process {
	t.Helper()
	p := &process{t: t, name: "gateway", bin: bin, env: baseEnv(dbURL), listeners: gatewayListeners, healthOn: "http"}
	for name, svc := range services {
		p.env = append(p.env, dependency(name, svc))
	}
	p.start()
	p.url = "http://" + p.addrs["http"]
	return p
}

// start runs the binary until healthy: on port 0 the first time, then on the addresses it got.
func (p *process) start() {
	p.t.Helper()
	env := slices.Clone(p.env)
	for _, name := range slices.Sorted(maps.Keys(p.listeners)) {
		addr := p.addrs[name]
		if addr == "" {
			addr = "127.0.0.1:0"
		}
		env = append(env, p.listeners[name]+"="+addr)
	}
	from := p.out.Len()
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
		if p.addrs == nil {
			if addrs := p.out.listening(from); len(addrs) == len(p.listeners) {
				p.addrs = addrs
			}
		}
		if p.addrs != nil && p.healthy() {
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
	res, err := http.Get("http://" + p.addrs[p.healthOn] + "/healthz")
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

// processStack runs every service and the gateway as their own processes, by name; world adds to the world's settings.
func processStack(t *testing.T, world ...string) (*stack, map[string]*process) {
	t.Helper()
	bins := buildBinaries(t)
	dbURL := cluster.URL(t)
	procs := map[string]*process{}
	for _, name := range []string{"item", "account", "social"} {
		procs[name] = startServiceProcess(t, name, bins[name], dbURL)
	}
	procs["character"] = startServiceProcess(t, "character", bins["character"], dbURL, dependency("item", procs["item"]))
	world = append([]string{dependency("character", procs["character"]), dependency("item", procs["item"]), dependency("social", procs["social"])}, world...)
	procs["world"] = startServiceProcess(t, "world", bins["world"], dbURL, world...)
	gateway := startGateway(t, bins["gateway"], dbURL, maps.Clone(procs))
	procs["gateway"] = gateway
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return &stack{t: t, url: gateway.url, db: db}, procs
}
