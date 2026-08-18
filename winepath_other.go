//go:build !windows

package main

// On non-Windows targets Wine path translation is never relevant: f4 talks
// to a real POSIX filesystem directly. These stubs exist only so the
// winepath_windows.go API compiles on every GOOS in the CI matrix.

// WineAvailable always reports false outside Windows.
func WineAvailable() bool { return false }

// WineHostOS always returns "" outside Windows.
func WineHostOS() string { return "" }

// WineDOSFromUnix is a no-op outside Windows.
func WineDOSFromUnix(unixPath string) (string, bool) { return "", false }

// WineUnixFromDOS is a no-op outside Windows.
func WineUnixFromDOS(dosPath string) (string, bool) { return "", false }
