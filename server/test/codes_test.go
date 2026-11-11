package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// codesByName maps a failure's name to its number, read from the file the gateway's tests keep current.
var codesByName = func() map[string]int32 {
	raw, err := os.ReadFile("../testdata/errors/codes.json")
	if err != nil {
		panic(err)
	}
	var rows []struct {
		Code int32
		Name string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		panic(err)
	}
	byName := make(map[string]int32, len(rows))
	for _, r := range rows {
		byName[r.Name] = r.Code
	}
	return byName
}()

// code is the number a failure goes by; an unknown name panics so a typo cannot pass silently.
func code(name string) int32 {
	c, ok := codesByName[name]
	if !ok {
		panic(fmt.Sprintf("no failure named %s in testdata/errors/codes.json", name))
	}
	return c
}

// The edge answers a few refusals itself; each must be a catalog code with that code's status and message.
func TestTheEdgesOwnRefusalsAreCatalogCodes(t *testing.T) {
	conf, err := os.ReadFile("../../infra/nginx/finga.conf")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../testdata/errors/codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Code    int32
		Status  int
		Message string
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	bodies := regexp.MustCompile(`return (\d{3}) '(\{[^']*\})\\n';`).FindAllSubmatch(conf, -1)
	if len(bodies) == 0 {
		t.Fatal("found no JSON body in finga.conf: is the pattern broken?")
	}
	for _, m := range bodies {
		var body struct {
			Code    int32
			Message string
		}
		if err := json.Unmarshal(m[2], &body); err != nil {
			t.Errorf("%s: %v; a code is a number", m[2], err)
			continue
		}
		status, _ := strconv.Atoi(string(m[1]))
		found := false
		for _, r := range rows {
			if r.Code == body.Code {
				found = true
				if r.Status != status || r.Message != body.Message {
					t.Errorf("%s answers %d %q; the catalog has %d %q", m[2], status, body.Message, r.Status, r.Message)
				}
			}
		}
		if !found {
			t.Errorf("%s: %d is not in the catalog", m[2], body.Code)
		}
	}
}
