//go:build !linux && !darwin && !windows

package main

// goffi does not support these targets. Their GUI backends already perform
// their own platform checks, so f4 must not import goffi just to preflight
// them.
func ffiAvailableForGUI() bool {
	return true
}
