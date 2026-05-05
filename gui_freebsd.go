//go:build freebsd

package main

import (
	"fmt"
)

func RunGui(backend string) error {
	return fmt.Errorf("GUI mode is currently not supported on this platform")
}
