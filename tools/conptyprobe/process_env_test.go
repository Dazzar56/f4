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
	text := syscall.UTF16ToString(b)
	for _, want := range []string{"F4_WIN_REFLOW=probe", "KEEP=yes", "PATH=new"} {
		if !strings.Contains(text, want+"\x00") && !strings.HasSuffix(text, want) {
			t.Fatalf("environment block %q lacks %q", text, want)
		}
	}
	if strings.Contains(strings.ToUpper(text), "WT_SESSION=") || strings.Contains(text, "Path=old") {
		t.Fatalf("environment block kept a removed/replaced value: %q", text)
	}
}
