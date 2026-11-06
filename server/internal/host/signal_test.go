package host_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
)

// hung is a process whose shutdown never finishes.
type hung struct{}

func (hung) Run(ctx context.Context) error {
	<-ctx.Done()
	fmt.Println("draining")
	time.Sleep(time.Hour)
	return nil
}

func (hung) Close() {}

// A second signal ends a shutdown that hangs, as an operator pressing Ctrl-C again expects.
func TestASecondSignalStopsAHungShutdown(t *testing.T) {
	if os.Getenv("FINGA_HOST_HUNG") == "1" {
		os.Exit(host.Main("shop", func(context.Context, shopConfig, *slog.Logger, *prometheus.Registry) (host.Process, error) {
			return hung{}, nil
		}))
	}
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM to send")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestASecondSignalStopsAHungShutdown$")
	cmd.Env = append(os.Environ(), "FINGA_HOST_HUNG=1")
	cmd.Dir = "testdata"
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := bufio.NewScanner(out)
	waitFor := func(text string) {
		t.Helper()
		for lines.Scan() {
			if strings.Contains(lines.Text(), text) {
				return
			}
		}
		t.Fatalf("the process never printed %q", text)
	}
	waitFor(`"msg":"starting"`)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitFor("draining")
	_ = cmd.Process.Signal(syscall.SIGTERM)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("the process exited with %v", err)
		}
		if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Fatalf("the process exited with %v, want killed by the second SIGTERM", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second SIGTERM did not stop the process")
	}
}
