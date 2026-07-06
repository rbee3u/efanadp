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
