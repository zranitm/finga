package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
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
