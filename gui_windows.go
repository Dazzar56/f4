//go:build windows

package main

import (
	"fmt"
	"os"
)

func RunGui() {
	fmt.Fprintf(os.Stderr, "GUI mode is currently not supported on Windows.\n")
	os.Exit(1)
}
