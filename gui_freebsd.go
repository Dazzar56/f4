//go:build freebsd

package main

import (
	"fmt"
	"os"
)

func RunGui() {
	fmt.Fprintf(os.Stderr, "GUI mode is currently not supported on FreeBSD.\n")
	os.Exit(1)
}