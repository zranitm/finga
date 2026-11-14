package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"finga/pkg/pgtest"
)

// The test binary stands in for the command: FINGA_PGTEST_CHILD says what it does.
func TestMain(m *testing.M) {
	switch os.Getenv("FINGA_PGTEST_CHILD") {
	case "":
		os.Exit(m.Run())
	case "check": // exits with FINGA_PGTEST_EXIT if it was given the server FINGA_PGTEST_WANT names
		if os.Getenv(pgtest.EnvURL) != os.Getenv("FINGA_PGTEST_WANT") {
			os.Exit(9)
		}
		code, _ := strconv.Atoi(os.Getenv("FINGA_PGTEST_EXIT"))
		os.Exit(code)
	case "ping": // reaches the server, and leaves its URL in FINGA_PGTEST_OUT
		conn, err := pgx.Connect(context.Background(), os.Getenv(pgtest.EnvURL))
		if err != nil || conn.Ping(context.Background()) != nil {
			os.Exit(8)
		}
		_ = conn.Close(context.Background())
		if os.WriteFile(os.Getenv("FINGA_PGTEST_OUT"), []byte(os.Getenv(pgtest.EnvURL)), 0o600) != nil {
			os.Exit(7)
		}
		os.Exit(0)
	case "wait": // says it started in FINGA_PGTEST_OUT, then runs until a signal ends it
		if os.WriteFile(os.Getenv("FINGA_PGTEST_OUT"), nil, 0o600) != nil {
			os.Exit(7)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

// child has the test binary act as the command, in the mode given.
func child(t *testing.T, mode string) []string {
	t.Setenv("FINGA_PGTEST_CHILD", mode)
	return []string{os.Args[0]}
}

// A server PGTEST_URL names is passed on as it is, and the command's exit code is pgtest's.
func TestTheCommandGetsTheNamedServerAndItsExitCode(t *testing.T) {
	const named = "postgres://pgtest:pgtest@db.example:5433/postgres"
	t.Setenv(pgtest.EnvURL, named)
	t.Setenv("FINGA_PGTEST_WANT", named)
	for _, code := range []int{0, 3} {
		t.Setenv("FINGA_PGTEST_EXIT", strconv.Itoa(code))
		if got := run(child(t, "check")); got != code {
			t.Errorf("exit code %d, want the command's %d", got, code)
		}
	}
}

func TestACommandThatCannotStartFails(t *testing.T) {
	t.Setenv(pgtest.EnvURL, "postgres://pgtest:pgtest@db.example:5433/postgres")
	if got := run([]string{filepath.Join(t.TempDir(), "missing")}); got != 1 {
		t.Fatalf("exit code %d, want 1", got)
	}
}

// A SIGTERM sent to pgtest alone ends the command, so an interrupted run still stops its server.
func TestASignalReachesTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM to send")
	}
	t.Setenv(pgtest.EnvURL, "postgres://pgtest:pgtest@db.example:5433/postgres")
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("FINGA_PGTEST_OUT", started)
	go func() {
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(started); err == nil {
				self, _ := os.FindProcess(os.Getpid())
				_ = self.Signal(syscall.SIGTERM)
				return
			}
		}
	}()
	begun := time.Now()
	if got := run(child(t, "wait")); got != 1 {
		t.Fatalf("exit code %d, want 1 for a command a signal ended", got)
	}
	if took := time.Since(begun); took > 15*time.Second {
		t.Fatalf("the command ran %s: the signal never reached it", took)
	}
}

// Without PGTEST_URL, pgtest starts a server the command reaches, and removes it once the command ends.
func TestWithoutAURLAServerComesAndGoes(t *testing.T) {
	t.Setenv(pgtest.EnvURL, "")
	out := filepath.Join(t.TempDir(), "url")
	t.Setenv("FINGA_PGTEST_OUT", out)
	if got := run(child(t, "ping")); got != 0 {
		t.Fatalf("exit code %d, want 0 from a command that reached the server", got)
	}
	url, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, err := pgx.Connect(ctx, string(url)); err == nil {
		_ = conn.Close(ctx)
		t.Fatal("the server still answers after the command ended")
	}
}
