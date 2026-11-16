// Command finga runs the game: every module in one process.
package main

import (
	"os"

	"finga/internal/dependencies"
	"finga/internal/shared/host"
)

func main() {
	os.Exit(host.Main("finga", dependencies.Build))
}
