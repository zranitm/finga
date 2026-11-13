// Command goroutines is a made-up binary: it links linked and allowed, never unlinked.
package main

import (
	"finga/test/rules/testdata/goroutines/allowed"
	"finga/test/rules/testdata/goroutines/linked"
)

func main() {
	linked.Start()
	allowed.Start()
}
