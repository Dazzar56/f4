package main

import (
	"fmt"
	"os"
)

func RunGui() {
	fmt.Fprintf(os.Stderr, "GUI mode is currently only supported on Linux/X11.\n")
	os.Exit(1)
}
