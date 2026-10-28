package e2e_test

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// process is a binary running as its own process over the test database,
// the way the crash test needs it: it can be killed without warning and
// started again, on the same addresses.
type process struct {
	t      *testing.T
	name   string
	bin    string
	env    []string
	url    string // the game server's base URL
	grpc   string // a service's gRPC address
	health string // the /healthz URL that says it is up
	cmd    *exec.Cmd
	out    lockedBuffer // the process writes it from exec's copier goroutine while the test reads it
	done   chan struct{}
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

// buildBinaries builds the binaries the process tests run, as production
// builds them, and returns their paths by name.
func buildBinaries(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", dir, "./cmd/gameserver", "./services/item/cmd/item", "./services/account/cmd/account", "./services/social/cmd/social")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	bins := map[string]string{}
	for _, name := range []string{"gameserver", "item", "account", "social"} {
		bins[name] = filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			bins[name] += ".exe"
		}
	}
	return bins
}

// baseEnv is what every process gets: the committed defaults, the test
// database and the game data, overridden as Compose would.
func baseEnv(t *testing.T, dbURL string) []string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return append(os.Environ(),
		"CONFIG_PATH="+filepath.Join(root, "configs", "config.yaml"),
		"FINGA_DATABASE_URL="+dbURL,
		"FINGA_WORLD_DATA_DIR="+filepath.Join(root, "data"),
		"FINGA_TELEMETRY_LOG_LEVEL=warn",
	)
}

// startServiceProcess runs a service binary (item, account, ...) on free ports.
func startServiceProcess(t *testing.T, name, bin, dbURL string) *process {
	t.Helper()
	grpc, ops := freePort(t), freePort(t)
	p := &process{t: t, name: name, bin: bin, grpc: grpc, health: "http://" + ops + "/healthz"}
	key := "FINGA_SERVICES_" + strings.ToUpper(name)
	p.env = append(baseEnv(t, dbURL), key+"_LISTEN="+grpc, key+"_OPS="+ops)
	p.start()
	return p
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startGameserver runs the game server on addr, dialling the services.
func startGameserver(t *testing.T, bin, dbURL, addr string, services map[string]*process) *process {
	t.Helper()
	p := &process{t: t, name: "gameserver", bin: bin, url: "http://" + addr, health: "http://" + addr + "/healthz"}
	p.env = append(baseEnv(t, dbURL), "FINGA_SERVER_ADDR="+addr)
	for name, svc := range services {
		p.env = append(p.env, "FINGA_SERVICES_"+strings.ToUpper(name)+"_ADDR="+svc.grpc)
	}
	p.start()
	return p
}

func (p *process) start() {
	p.t.Helper()
	p.cmd = exec.Command(p.bin)
	p.cmd.Env = p.env
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
		res, err := http.Get(p.health)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-done:
			p.t.Fatalf("%s exited before it was healthy:\n%s", p.name, p.out.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	p.t.Fatalf("%s not healthy within 20 s:\n%s", p.name, p.out.String())
}

// kill ends the process without warning (TerminateProcess on Windows, SIGKILL
// elsewhere): no shutdown, no checkpoint, no flush.
func (p *process) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}
