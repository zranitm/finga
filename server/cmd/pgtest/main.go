// Command pgtest runs a command, such as go test, beside one Postgres that its test packages share through PGTEST_URL, instead of a container each.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"finga/pkg/pgtest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pgtest command [arg...]")
		os.Exit(2)
	}
	os.Exit(run(os.Args[1:]))
}

// run starts a server unless PGTEST_URL names one, runs the command with it, and returns the command's exit code; the server stops however the command ends.
func run(args []string) int {
	if os.Getenv(pgtest.EnvURL) == "" {
		server, err := pgtest.StartServer(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer func() { // testcontainers' reaper removes the container even if this process is killed
			if err := server.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "pgtest:", err)
			}
		}()
		if err := os.Setenv(pgtest.EnvURL, server.URL); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest:", err)
			return 1
		}
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM) // a Ctrl-C reaches the command too; a signal sent here alone is passed on
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "pgtest:", err)
		return 1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-signals:
			_ = cmd.Process.Signal(sig)
		case err := <-done:
			var exit *exec.ExitError
			switch {
			case err == nil:
				return 0
			case errors.As(err, &exit) && exit.ExitCode() > 0:
				return exit.ExitCode()
			default:
				fmt.Fprintln(os.Stderr, "pgtest:", err)
				return 1
			}
		}
	}
}
