package panics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"finga/pkg/panics"
)

func explode(v any) { panic(v) }

func captured(v any) (err error) {
	defer panics.Capture(&err)
	explode(v)
	return nil
}

func TestCaptureTurnsAPanicIntoAnErrorWithItsStack(t *testing.T) {
	err := captured("boom")
	e, ok := errors.AsType[*panics.Error](err)
	if !ok || e.Value != "boom" || err.Error() != "panic: boom" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(string(e.Stack), "panics_test.explode") {
		t.Fatalf("the stack lacks the panicking function:\n%s", e.Stack)
	}
}

func TestCaptureLeavesAReturnedErrorAlone(t *testing.T) {
	want := errors.New("plain")
	got := func() (err error) {
		defer panics.Capture(&err)
		return want
	}()
	if !errors.Is(got, want) {
		t.Fatalf("err = %v", got)
	}
	if _, ok := errors.AsType[*panics.Error](got); ok {
		t.Fatal("a returned error became a panic")
	}
}

func TestPanicNilIsCapturedToo(t *testing.T) {
	e, ok := errors.AsType[*panics.Error](captured(nil))
	if _, isNil := e.Value.(*runtime.PanicNilError); !ok || !isNil {
		t.Fatalf("panic(nil) = %#v", e)
	}
}

// counts is a panics.Counter for the test.
type counts map[string]int

func (c counts) Inc(flow string) { c[flow]++ }

func TestRecordLogsAndCountsByFlow(t *testing.T) {
	var buf bytes.Buffer
	n := counts{}
	rec := panics.NewRecorder(slog.New(slog.NewJSONHandler(&buf, nil)), n)
	e, _ := errors.AsType[*panics.Error](captured("boom"))
	rec.Record(context.Background(), panics.FlowWS, e, "conn", "c-1")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["level"] != "ERROR" || line["msg"] != "panic recovered" || line["flow"] != "ws" || line["panic"] != "boom" || line["conn"] != "c-1" ||
		!strings.Contains(line["stack"].(string), "panics_test.explode") {
		t.Fatalf("line %v", line)
	}
	if len(n) != 1 || n[panics.FlowWS] != 1 {
		t.Fatalf("counts %v, want one ws panic", n)
	}
}

func TestARecorderWithoutACounterOnlyLogs(t *testing.T) {
	var buf bytes.Buffer
	rec := panics.NewRecorder(slog.New(slog.NewJSONHandler(&buf, nil)), nil)
	rec.Record(context.Background(), panics.FlowStream, panics.Of("boom"))
	if !strings.Contains(buf.String(), `"flow":"stream"`) || !strings.Contains(buf.String(), `"panic":"boom"`) {
		t.Fatalf("log %q", buf.String())
	}
}

func TestDoRecordsAPanicAndReturns(t *testing.T) {
	var buf bytes.Buffer
	n := counts{}
	rec := panics.NewRecorder(slog.New(slog.NewJSONHandler(&buf, nil)), n)
	rec.Do(context.Background(), panics.FlowJob, func() { explode("bad job") }, "job", "pickup")
	if n[panics.FlowJob] != 1 || !strings.Contains(buf.String(), `"job":"pickup"`) || !strings.Contains(buf.String(), `"panic":"bad job"`) {
		t.Fatalf("counts %v, log %q", n, buf.String())
	}
}

func TestGoRecordsAPanicOnItsOwnGoroutine(t *testing.T) {
	recorded := make(chan string, 1)
	rec := panics.NewRecorder(slog.New(slog.DiscardHandler), counterFunc(func(flow string) { recorded <- flow }))
	rec.Go(context.Background(), panics.FlowJob, func() { explode("bad transfer") })
	if flow := <-recorded; flow != panics.FlowJob {
		t.Fatalf("flow %q", flow)
	}
}

type counterFunc func(flow string)

func (f counterFunc) Inc(flow string) { f(flow) }
