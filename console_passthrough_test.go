package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/unxed/vtui"
)

func TestMutedPTY_SilencesWrites(t *testing.T) {
	mock := &mockPty{}
	muted := mutedPTY{backend: mock}

	payload := []byte("\x1b[?1;2c")
	n, err := muted.Write(payload)
	if err != nil {
		t.Fatalf("muted.Write error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("muted.Write returned n=%d, want %d", n, len(payload))
	}
	if len(mock.written) != 0 {
		t.Fatalf("mutedPTY leaked write to underlying backend: %q", string(mock.written))
	}
}

func TestHostConsole_Transitions(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	var out bytes.Buffer
	scr.Writer = &out
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeHost
	pf.ResizeConsole(80, 25)

	// 1. Enter host console
	pf.enterHostConsole()
	if !pf.isHostConsoleActive() {
		t.Fatal("hostConsoleActive should be true after enterHostConsole")
	}
	if !pf.IsBusy() {
		t.Fatal("pf.IsBusy() should be true in host console mode to suppress UI redraws")
	}

	// 2. Leave host console
	out.Reset()
	pf.leaveHostConsole()
	if pf.isHostConsoleActive() {
		t.Fatal("hostConsoleActive should be false after leaveHostConsole")
	}
	if pf.IsBusy() {
		t.Fatal("pf.IsBusy() should be false after leaveHostConsole")
	}

	// Verify protective reset sequence was written via passthrough
	written := out.String()
	if !strings.Contains(written, "\x1b[?1000l") || !strings.Contains(written, "\x1b[?2004l") || !strings.Contains(written, "\x1b[0m") {
		t.Fatalf("leaveHostConsole missing protective reset sequences: %q", written)
	}
}

func TestChildEnv_HostModeLeavesTERMUntouched(t *testing.T) {
	oldProbeGUI := probeGUIBackend
	oldProbeTTY := probeHostTTY
	oldProbePTY := probePTYUsable
	defer func() {
		probeGUIBackend = oldProbeGUI
		probeHostTTY = oldProbeTTY
		probePTYUsable = oldProbePTY
	}()

	probeGUIBackend = func() string { return "" }
	probeHostTTY = func() bool { return true }
	probePTYUsable = func() bool { return true }

	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleMode = "host"

	origTerm := os.Getenv("TERM")
	defer os.Setenv("TERM", origTerm)
	os.Setenv("TERM", "xterm-256color")

	env := terminalChildEnv()
	if envHasKey(env, "KITTY_WINDOW_ID") {
		t.Errorf("host mode must not advertise KITTY_WINDOW_ID: %v", env)
	}
	if !envHas(env, "TERM=xterm-256color") {
		t.Errorf("host mode must preserve host TERM: %v", env)
	}
	if !envHas(env, "F4_NESTED=1") || !envHas(env, "TERM_PROGRAM=f4") {
		t.Errorf("host mode must export F4_NESTED and TERM_PROGRAM: %v", env)
	}
}
