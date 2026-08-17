package main

import (
	"os"
	"runtime"
	"strings"

	"github.com/unxed/vtui"
	"golang.org/x/term"
)

// ShellMode defines how shell commands and console interactions are executed.
type ShellMode int

const (
	// ShellModeOwn uses the internal terminal emulator (PTY -> parser -> grid -> vtui).
	ShellModeOwn ShellMode = iota
	// ShellModeHost uses host terminal passthrough with internal mirror.
	ShellModeHost
	// ShellModeSimpleInline runs commands via Suspend/exec/Resume directly in host console.
	ShellModeSimpleInline
	// ShellModeSimpleCaptured runs commands with captured output in an f4 dialog.
	ShellModeSimpleCaptured
)

func (m ShellMode) String() string {
	switch m {
	case ShellModeHost:
		return "host"
	case ShellModeSimpleInline:
		return "simple-inline"
	case ShellModeSimpleCaptured:
		return "simple-captured"
	default:
		return "own"
	}
}

// ShellModeConfig carries user configuration for shell mode selection.
type ShellModeConfig struct {
	ConsoleMode      string // "own" | "host"
	ConsoleOverlayUI bool
}

// Environment probe functions, customizable for testing.
var (
	probeGUIBackend = func() string { return vtui.ActiveBackend() }
	probeHostTTY    = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
	probePTYUsable  = func() bool { return !vtui.IsWine() }
	probeGOOS       = func() string { return runtime.GOOS }
)

// resolveShellMode calculates the effective shell execution mode based on
// environment capabilities and user preference according to CONSOLE_MODES.md §4.1.
func resolveShellMode(cfg ShellModeConfig) ShellMode {
	if !probePTYUsable() {
		if probeHostTTY() && probeGOOS() == "windows" {
			return ShellModeSimpleInline
		}
		return ShellModeSimpleCaptured
	}
	if !strings.EqualFold(cfg.ConsoleMode, "host") {
		return ShellModeOwn
	}
	if probeGUIBackend() != "" {
		return ShellModeOwn
	}
	if !probeHostTTY() {
		return ShellModeOwn
	}
	return ShellModeHost
}
