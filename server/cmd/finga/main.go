// Command finga runs the game: every module in one process.
package main

import (
	"os"

	"finga/cmd/finga/app"
	"finga/internal/host"
)

func main() {
	os.Exit(host.Main("finga", app.Build))
}
