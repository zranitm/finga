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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/config"
	"finga/pkg/dbx"
)

// process is a binary in its own process, so it can be killed and restarted on the same addresses.
type process struct {
	t         *testing.T
	name      string
	bin       string
	env       []string
	listeners map[string]string // listener name: the setting of its address
	addrs     map[string]string // taken on the first start, kept for restarts
	url       string            // the players' base URL
	cmd       *exec.Cmd
	out       lockedBuffer // written by exec's copier goroutine while the test reads it
	done      chan struct{}
}

// listeners are the game binary's, each with the setting of its address.
var listeners = map[string]string{"http": "FINGA_HTTP_ADDR", "ops": "FINGA_OPS_ADDR"}

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

// binDir holds the game binary the process tests build, once for the package; TestMain removes it.
var binDir string

var buildOnce = sync.OnceValues(func() ([]byte, error) {
	build := exec.Command("go", "build", "-o", binDir, "./cmd/finga")
	build.Dir = ".."
	return build.CombinedOutput()
})

// startGame builds the game binary once and runs it on free ports over the test database and the game data in data; extra adds to its settings.
func startGame(t *testing.T, dbURL, data string, extra ...string) *process {
	t.Helper()
	if out, err := buildOnce(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	bin := filepath.Join(binDir, "finga")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	env := append(os.Environ(), "FINGA_DATABASE_URL="+dbURL, "FINGA_DATA_DIR="+data, "FINGA_LOG_LEVEL=info") // info logs what a failed start shows
	p := &process{t: t, name: "finga", bin: bin, env: append(env, extra...), listeners: listeners}
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

// processStack runs the game binary over the game data in data; extra adds to its settings.
func processStack(t *testing.T, data string, extra ...string) (*stack, *process) {
	t.Helper()
	dbURL := cluster.URL(t)
	abs, err := filepath.Abs(data) // the binary runs in server/
	if err != nil {
		t.Fatal(err)
	}
	game := startGame(t, dbURL, abs, extra...)
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	items := itemsOver(t, config.Config{DataDir: data, Database: dbx.Config{URL: dbURL, MaxConns: 4}, Item: itemConfig})
	return &stack{t: t, url: game.url, db: db, items: items, data: data, stop: func() {}}, game
}
