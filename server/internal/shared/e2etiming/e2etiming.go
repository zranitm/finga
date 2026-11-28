// Package e2etiming appends measurement lines to a file the CI job uploads; it exists for a measuring PR only.
package e2etiming

import (
	"fmt"
	"os"
	"sync"
)

var mu sync.Mutex

// Logf appends a line to $GITHUB_WORKSPACE/server/e2etiming.txt, if the variable is set.
func Logf(format string, args ...any) {
	root := os.Getenv("GITHUB_WORKSPACE")
	if root == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(root+"/server/e2etiming.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "E2ETIMING "+format+"\n", args...)
}
