// Command efanadp runs a local SOCKS5 adapter that carries traffic through
// decrypted efanapp x365 nodes. With no arguments it launches the interactive
// TUI; see internal/app for the supported subcommands.
package main

import (
	"fmt"
	"os"

	"github.com/rbee3u/efanadp/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "efanadp:", err)
		os.Exit(1)
	}
}
