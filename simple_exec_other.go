//go:build !windows

package main

func modMsvcrtProcImpl() interface {
	Call(...uintptr) (uintptr, uintptr, error)
} {
	return nil
}

func captureHostConsoleBufferImpl(w, h int) {}
func restoreHostConsoleBufferImpl()         {}

func hostConsoleBufferMatches(w, h int) bool { return false }
