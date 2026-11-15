// Command testtimes runs go test -json, prints what go test prints without it, and writes when each package and test ran to a TSV file.
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

func main() {
	out := flag.String("o", "go-test-times.tsv", "the TSV file the times go to")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: testtimes [-o file] go test -json [flag...] [package...]")
		os.Exit(2)
	}
	os.Exit(run(flag.Args(), *out))
}

// run runs the command, echoing its output, writes the times to out however it ends, and returns the command's exit code.
func run(args []string, out string) int {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testtimes:", err)
		return 1
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM) // a Ctrl-C reaches the command too; a signal sent here alone is passed on
	defer signal.Stop(signals)
	t := newTimes(time.Now())
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "testtimes:", err)
		return 1
	}
	go func() {
		for sig := range signals {
			_ = cmd.Process.Signal(sig)
		}
	}()
	readErr := t.read(stdout, os.Stdout)
	code := exitCode(cmd.Wait())
	if readErr != nil {
		fmt.Fprintln(os.Stderr, "testtimes:", readErr)
		code = cmp.Or(code, 1)
	}
	if err := t.save(out); err != nil {
		fmt.Fprintln(os.Stderr, "testtimes:", err)
		code = cmp.Or(code, 1)
	}
	return code
}

func exitCode(err error) int {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit) && exit.ExitCode() > 0:
		return exit.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "testtimes:", err)
		return 1
	}
}

// event is a line go test -json writes (go doc test2json).
type event struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Output  string
}

// span is when a package's tests or a test ran, in seconds from the command's start, and how it ended; a parallel test's runs from when it went on.
type span struct {
	pkg, test  string
	start, end float64
	result     string
}

type times struct {
	begin time.Time
	spans map[[2]string]*span
	held  map[[2]string][]byte // output not shown yet: a test's until it fails, a package's until it ends
}

func newTimes(begin time.Time) *times {
	return &times{begin: begin, spans: map[[2]string]*span{}, held: map[[2]string][]byte{}}
}

// read prints to w what go test prints without -json, and keeps when each package and test began and ended; a line that isn't an event is printed as it is.
func (t *times) read(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if werr := t.line(line, w); werr != nil {
				return werr
			}
		}
		if errors.Is(err, io.EOF) {
			return t.unfinished(w)
		}
		if err != nil {
			return err
		}
	}
}

func (t *times) line(line []byte, w io.Writer) error {
	var e event
	if json.Unmarshal(line, &e) != nil || e.Action == "" {
		_, err := w.Write(line)
		return err
	}
	if e.Package == "" {
		_, err := io.WriteString(w, e.Output) // a build's output, which names no package
		return err
	}
	at := e.Time.Sub(t.begin).Seconds()
	key, pkg := [2]string{e.Package, e.Test}, [2]string{e.Package, ""}
	s := t.spans[key]
	if s == nil {
		s = &span{pkg: e.Package, test: e.Test, start: at}
		t.spans[key] = s
	}
	t.held[key] = append(t.held[key], e.Output...)
	switch e.Action {
	case "cont":
		s.start = at
	case "pass", "skip":
		s.end, s.result = at, e.Action
		if e.Test != "" {
			delete(t.held, key)
			return nil
		}
		last := lastLine(t.held[key]) // a package that passed shows go test's own line alone, as without -json
		delete(t.held, key)
		_, err := io.WriteString(w, last)
		return err
	case "fail":
		s.end, s.result = at, e.Action
		if e.Test != "" {
			t.held[pkg] = append(t.held[pkg], t.held[key]...) // shown with its package's output, where it failed
			delete(t.held, key)
			return nil
		}
		return t.show(w, key, func(line string) bool { return !strings.HasPrefix(line, "=== ") }) // -json's own RUN, PAUSE and CONT lines
	}
	return nil
}

// lastLine is out's last line: for a package that passed, go test's ok or no-test-files line.
func lastLine(out []byte) string {
	s := strings.TrimSuffix(string(out), "\n")
	if s == "" {
		return ""
	}
	return s[strings.LastIndexByte(s, '\n')+1:] + "\n"
}

// show prints the lines key's held output keeps, and lets it go.
func (t *times) show(w io.Writer, key [2]string, keep func(string) bool) error {
	for line := range strings.Lines(string(t.held[key])) {
		if keep(line) {
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
	}
	delete(t.held, key)
	return nil
}

// unfinished prints the output of the packages and tests that never ended, as when go test is stopped or a test hangs, packages first.
func (t *times) unfinished(w io.Writer) error {
	keys := slices.SortedFunc(maps.Keys(t.held), func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(min(len(a[1]), 1), min(len(b[1]), 1)), cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
	for _, key := range keys {
		if err := t.show(w, key, func(string) bool { return true }); err != nil {
			return err
		}
	}
	return nil
}

// save writes the packages, then the tests, each slowest first; one that never ended, as when go test is stopped, has no result.
func (t *times) save(path string) error {
	rows := slices.Collect(maps.Values(t.spans))
	slices.SortFunc(rows, func(a, b *span) int {
		return cmp.Or(
			cmp.Compare(min(len(a.test), 1), min(len(b.test), 1)), // packages first
			cmp.Compare(b.seconds(), a.seconds()),
			cmp.Compare(a.pkg, b.pkg),
			cmp.Compare(a.test, b.test),
		)
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	fmt.Fprintln(bw, "seconds\tstart\tend\tresult\tpackage\ttest")
	for _, s := range rows {
		fmt.Fprintf(bw, "%.2f\t%.2f\t%.2f\t%s\t%s\t%s\n", s.seconds(), s.start, s.end, s.result, s.pkg, s.test)
	}
	return errors.Join(bw.Flush(), f.Close())
}

func (s *span) seconds() float64 {
	if s.result == "" {
		return 0
	}
	return s.end - s.start
}
