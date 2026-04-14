//go:build windows

package vfs

import "os"

func RunSudoAskpass() {
	// Not used on Windows
	os.Exit(1)
}

func (c *SudoClient) runAskpassServer(path string) {
	// Not used on Windows
}

func (c *SudoClient) RunOnUI(fn func()) {
	// Not used on Windows
}