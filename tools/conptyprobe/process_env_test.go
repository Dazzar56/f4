//go:build windows

package main

import (
	"strings"
	"syscall"
	"testing"
)

func TestWindowsEnvironmentBlockOverridesAndRemoves(t *testing.T) {
	b := windowsEnvironmentBlock(
		[]string{"Path=old", "WT_SESSION=old", "KEEP=yes"},
		map[string]string{"PATH": "new", "WT_SESSION": "", "F4_WIN_REFLOW": "probe"},
	)
	// The block is NUL-separated and double-NUL terminated; UTF16ToString
	// stops at the first NUL, so it must be split into entries first.
	var entries []string
	start := 0
	for i, u := range b {
		if u == 0 {
			if i > start {
				entries = append(entries, syscall.UTF16ToString(b[start:i]))
			}
			start = i + 1
		}
	}
	text := strings.Join(entries, "\n")
	for _, want := range []string{"F4_WIN_REFLOW=probe", "KEEP=yes", "PATH=new"} {
		found := false
		for _, e := range entries {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("environment block %q lacks %q", text, want)
		}
	}
	if strings.Contains(strings.ToUpper(text), "WT_SESSION=") || strings.Contains(text, "Path=old") {
		t.Fatalf("environment block kept a removed/replaced value: %q", text)
	}
}
