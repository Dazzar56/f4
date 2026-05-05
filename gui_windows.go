//go:build windows

package main

import (
	"fmt"
	"os"
)

func RunGui() error {
	return fmt.Errorf("GUI mode is currently not supported on this platform")
}
