//go:build windows

package vfs

import "os"

func RunSudoAskpass() {
	// Not used on Windows
	os.Exit(1)
}