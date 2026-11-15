package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var begin = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

// The test binary stands in for go test when FINGA_TESTTIMES_CHILD is set: it writes a failing package's events and exits 1.
func TestMain(m *testing.M) {
	if os.Getenv("FINGA_TESTTIMES_CHILD") == "" {
		os.Exit(m.Run())
	}
	now := time.Now()
	for _, e := range []event{
		{Time: now, Action: "start", Package: "finga/x"},
		{Time: now.Add(time.Second), Action: "output", Package: "finga/x", Output: "the stand-in's output\n"},
		{Time: now.Add(time.Second), Action: "fail", Package: "finga/x"},
	} {
		b, _ := json.Marshal(e)
		fmt.Println(string(b))
	}
	os.Exit(1)
}

func line(t *testing.T, at float64, action, pkg, test, output string) string {
	t.Helper()
	b, err := json.Marshal(event{Time: begin.Add(time.Duration(at * float64(time.Second))), Action: action, Package: pkg, Test: test, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestItPrintsWhatGoTestPrintsWithoutJSON(t *testing.T) {
	stream := strings.Join([]string{
		"# a line that isn't an event\n",
		line(t, 1, "start", "finga/a", "", ""),
		line(t, 1, "run", "finga/a", "TestQuiet", ""),
		line(t, 1, "output", "finga/a", "TestQuiet", "=== RUN   TestQuiet\n"),
		line(t, 1, "output", "finga/a", "TestQuiet", "    a_test.go:3: a log a pass hides\n"),
		line(t, 1, "output", "finga/a", "TestQuiet", "--- PASS: TestQuiet (0.00s)\n"),
		line(t, 1, "pass", "finga/a", "TestQuiet", ""),
		line(t, 1, "output", "finga/a", "", "PASS\n"),
		line(t, 1, "output", "finga/a", "", "ok  \tfinga/a\t0.1s\n"),
		line(t, 1, "pass", "finga/a", "", ""),
		line(t, 2, "start", "finga/c", "", ""),
		line(t, 2, "output", "finga/c", "", "a log from TestMain\n"),
		line(t, 2, "run", "finga/c", "TestBroken", ""),
		line(t, 2, "output", "finga/c", "TestBroken", "=== RUN   TestBroken\n"),
		line(t, 2, "output", "finga/c", "TestBroken", "    c_test.go:9: broken\n"),
		line(t, 2, "output", "finga/c", "TestBroken", "--- FAIL: TestBroken (0.00s)\n"),
		line(t, 2, "fail", "finga/c", "TestBroken", ""),
		line(t, 2, "output", "finga/c", "", "FAIL\n"),
		line(t, 2, "output", "finga/c", "", "FAIL\tfinga/c\t0.1s\n"),
		line(t, 2, "fail", "finga/c", "", ""),
		line(t, 3, "start", "finga/d", "", ""),
		line(t, 3, "output", "finga/d", "", "?   \tfinga/d\t[no test files]\n"),
		line(t, 3, "skip", "finga/d", "", ""),
		line(t, 3, "start", "finga/e", "", ""),
		line(t, 3, "output", "finga/e", "", "\tfinga/e\t\tcoverage: 0.0% of statements\n"),
		line(t, 3, "pass", "finga/e", "", ""),
		line(t, 4, "start", "finga/b", "", ""),
		line(t, 4, "run", "finga/b", "TestStuck", ""),
		line(t, 4, "output", "finga/b", "TestStuck", "=== RUN   TestStuck\n"),
	}, "")
	var printed bytes.Buffer
	if err := newTimes(begin).read(strings.NewReader(stream), &printed); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"# a line that isn't an event",
		"ok  \tfinga/a\t0.1s",
		"a log from TestMain",
		"    c_test.go:9: broken",
		"--- FAIL: TestBroken (0.00s)",
		"FAIL",
		"FAIL\tfinga/c\t0.1s",
		"?   \tfinga/d\t[no test files]",
		"\tfinga/e\t\tcoverage: 0.0% of statements", // -cover's line for a package with no tests, which passes
		"=== RUN   TestStuck",                       // the test that never ended, shown when go test stops
		"",
	}, "\n")
	if printed.String() != want {
		t.Errorf("printed:\n%s\nwant:\n%s", printed.String(), want)
	}
}

func TestEachPackageAndTestIsTimedFromWhenItWentOn(t *testing.T) {
	stream := strings.Join([]string{
		line(t, 2, "start", "finga/a", "", ""),
		line(t, 2.5, "run", "finga/a", "TestSlow", ""),
		line(t, 2.5, "output", "finga/a", "TestSlow", "=== RUN   TestSlow\n"),
		line(t, 2.5, "pause", "finga/a", "TestSlow", ""),
		line(t, 3, "run", "finga/a", "TestQuick", ""),
		line(t, 3.25, "pass", "finga/a", "TestQuick", ""),
		line(t, 4, "cont", "finga/a", "TestSlow", ""),
		line(t, 9, "pass", "finga/a", "TestSlow", ""),
		line(t, 9, "output", "finga/a", "", "ok  \tfinga/a\t7.0s\n"),
		line(t, 9, "pass", "finga/a", "", ""),
		line(t, 5, "start", "finga/b", "", ""),
		line(t, 6, "run", "finga/b", "TestStuck", ""),
	}, "")
	tm := newTimes(begin)
	if err := tm.read(strings.NewReader(stream), io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "times.tsv")
	if err := tm.save(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"seconds\tstart\tend\tresult\tpackage\ttest",
		"7.00\t2.00\t9.00\tpass\tfinga/a\t",
		"0.00\t5.00\t0.00\t\tfinga/b\t",
		"5.00\t4.00\t9.00\tpass\tfinga/a\tTestSlow",
		"0.25\t3.00\t3.25\tpass\tfinga/a\tTestQuick",
		"0.00\t6.00\t0.00\t\tfinga/b\tTestStuck",
		"",
	}, "\n")
	if string(got) != want {
		t.Errorf("times:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunReturnsTheCommandsExitCodeAndSavesTheTimesOfAFailedRun(t *testing.T) {
	t.Setenv("FINGA_TESTTIMES_CHILD", "1")
	path := filepath.Join(t.TempDir(), "times.tsv")
	if code := run([]string{os.Args[0]}, path); code != 1 {
		t.Errorf("exit code %d; want the command's 1", code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n"); len(rows) != 2 || !strings.HasSuffix(rows[1], "\tfail\tfinga/x\t") {
		t.Errorf("times %q; want the package's failed run", got)
	}
}
