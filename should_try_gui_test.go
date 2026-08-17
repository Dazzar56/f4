package main

import (
	"runtime"
	"testing"
)

func TestShouldTryGui_WindowsAndWineDefaultToConsole(t *testing.T) {
	if runtime.GOOS == "windows" {
		if shouldTryGui() {
			t.Error("shouldTryGui() on Windows/Wine must return false by default")
		}
	}
}
