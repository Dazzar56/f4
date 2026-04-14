//go:build windows

package vfs

// RunSudoDispatcher is a stub for Windows. Privilege elevation on Windows
// is handled via User Account Control (UAC) and COM/ShellExecute elevation,
// not via a background socket dispatcher.
func RunSudoDispatcher(sockPath string) {
	// Stub
}