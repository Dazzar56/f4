package main

import (
	"testing"
)

func TestResolveShellMode_Matrix(t *testing.T) {
	oldProbeGUI := probeGUIBackend
	oldProbeTTY := probeHostTTY
	oldProbePTY := probePTYUsable
	oldProbeGOOS := probeGOOS
	defer func() {
		probeGUIBackend = oldProbeGUI
		probeHostTTY = oldProbeTTY
		probePTYUsable = oldProbePTY
		probeGOOS = oldProbeGOOS
	}()

	tests := []struct {
		name        string
		cfg         ShellModeConfig
		ptyUsable   bool
		hostTTY     bool
		guiBackend  string
		goos        string
		wantMode    ShellMode
		wantModeStr string
	}{
		{
			name:        "PTY unusable + Host TTY + Windows -> SimpleInline",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   false,
			hostTTY:     true,
			guiBackend:  "",
			goos:        "windows",
			wantMode:    ShellModeSimpleInline,
			wantModeStr: "simple-inline",
		},
		{
			name:        "PTY unusable + Host TTY + Linux -> SimpleCaptured",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   false,
			hostTTY:     true,
			guiBackend:  "",
			goos:        "linux",
			wantMode:    ShellModeSimpleCaptured,
			wantModeStr: "simple-captured",
		},
		{
			name:        "PTY unusable + GUI -> SimpleCaptured",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   false,
			hostTTY:     false,
			guiBackend:  "x11",
			goos:        "linux",
			wantMode:    ShellModeSimpleCaptured,
			wantModeStr: "simple-captured",
		},
		{
			name:        "PTY usable + Config own + Host TTY -> Own",
			cfg:         ShellModeConfig{ConsoleMode: "own"},
			ptyUsable:   true,
			hostTTY:     true,
			guiBackend:  "",
			goos:        "linux",
			wantMode:    ShellModeOwn,
			wantModeStr: "own",
		},
		{
			name:        "PTY usable + Config host + GUI backend -> Own",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   true,
			hostTTY:     true,
			guiBackend:  "gogpu",
			goos:        "linux",
			wantMode:    ShellModeOwn,
			wantModeStr: "own",
		},
		{
			name:        "PTY usable + Config host + No Host TTY -> Own",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   true,
			hostTTY:     false,
			guiBackend:  "",
			goos:        "linux",
			wantMode:    ShellModeOwn,
			wantModeStr: "own",
		},
		{
			name:        "PTY usable + Config host + Host TTY + No GUI -> Host",
			cfg:         ShellModeConfig{ConsoleMode: "host"},
			ptyUsable:   true,
			hostTTY:     true,
			guiBackend:  "",
			goos:        "linux",
			wantMode:    ShellModeHost,
			wantModeStr: "host",
		},
		{
			name:        "PTY usable + Config host (case insensitive) + Host TTY -> Host",
			cfg:         ShellModeConfig{ConsoleMode: "HoSt"},
			ptyUsable:   true,
			hostTTY:     true,
			guiBackend:  "",
			goos:        "windows",
			wantMode:    ShellModeHost,
			wantModeStr: "host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probePTYUsable = func() bool { return tt.ptyUsable }
			probeHostTTY = func() bool { return tt.hostTTY }
			probeGUIBackend = func() string { return tt.guiBackend }
			probeGOOS = func() string { return tt.goos }

			got := resolveShellMode(tt.cfg)
			if got != tt.wantMode {
				t.Errorf("resolveShellMode() = %v (%s), want %v (%s)", got, got.String(), tt.wantMode, tt.wantModeStr)
			}
			if got.String() != tt.wantModeStr {
				t.Errorf("ShellMode.String() = %q, want %q", got.String(), tt.wantModeStr)
			}
		})
	}
}
