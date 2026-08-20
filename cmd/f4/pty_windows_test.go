//go:build windows

package main

import (
	"testing"
)

func TestConPTYAvailable_DoesNotPanic(t *testing.T) {
	avail := conPTYAvailable()
	if !avail {
		pty, err := NewPTY()
		if err == nil {
			if pty != nil {
				pty.Close()
			}
			t.Fatal("NewPTY succeeded when conPTYAvailable() reported false")
		}
	}
}
