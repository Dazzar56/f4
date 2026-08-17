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
func TestHostConsole_PanelToggleAction(t *testing.T) {
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
	vtui.FrameManager.Push(pf)

	if !pf.showPanels {
		t.Fatal("panels should be visible initially")
	}

	// 1. Run Panel.Toggle -> should hide panels and enter host console
	if !RunAction("Panel.Toggle") {
		t.Fatal("Panel.Toggle action failed")
	}
	if pf.showPanels {
		t.Fatal("Panel.Toggle did not hide panels")
	}
	if !pf.isHostConsoleActive() {
		t.Fatal("hostConsoleActive should be true after toggling panels off in host mode")
	}

	// 2. Run Panel.Toggle again -> should show panels and leave host console
	if !RunAction("Panel.Toggle") {
		t.Fatal("Panel.Toggle second action failed")
	}
	if !pf.showPanels {
		t.Fatal("Panel.Toggle second action did not show panels")
	}
	if pf.isHostConsoleActive() {
		t.Fatal("hostConsoleActive should be false after toggling panels back on in host mode")
	}
}

func TestHostConsole_InputForwardingWhenIdle(t *testing.T) {
	pf := setupMockPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeHost
	pf.showPanels = false
	pf.enterHostConsole()

	mock := pf.pty.(*mockPty)
	mock.Reset()

	// Send key 'x'
	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    'x',
	})

	if got := mock.String(); got != "x" {
		t.Errorf("Host mode idle forwarding: got %q, want %q", got, "x")
	}
}

func TestHostConsole_CloseLeavesHostConsole(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	var out bytes.Buffer
	scr.Writer = &out
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	pf := NewPanelsFrame()
	pf.shellMode = ShellModeHost
	pf.enterHostConsole()
	if !pf.isHostConsoleActive() {
		t.Fatal("host console must be active before Close")
	}

	pf.Close()
	if pf.isHostConsoleActive() {
		t.Fatal("Close must leave host console")
	}
}
func TestHostConsole_OverlayLines(t *testing.T) {
	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()

	pf := NewPanelsFrame()
	defer pf.Close()

	// 1. ConsoleOverlayUI disabled -> 0 lines
	AppConfig.ConsoleOverlayUI = false
	if got := pf.overlayLines(); got != 0 {
		t.Errorf("overlayLines() with ConsoleOverlayUI=false = %d, want 0", got)
	}

	// 2. ConsoleOverlayUI enabled, showKeyBar = true -> 2 lines
	AppConfig.ConsoleOverlayUI = true
	pf.showKeyBar = true
	if got := pf.overlayLines(); got != 2 {
		t.Errorf("overlayLines() with showKeyBar=true = %d, want 2", got)
	}

	// 3. ConsoleOverlayUI enabled, showKeyBar = false -> 1 line
	pf.showKeyBar = false
	if got := pf.overlayLines(); got != 1 {
		t.Errorf("overlayLines() with showKeyBar=false = %d, want 1", got)
	}
}

func TestHostConsole_FarStyleScrollRegion(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	var out bytes.Buffer
	scr.Writer = &out
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleOverlayUI = true

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeHost
	pf.showKeyBar = true
	pf.ResizeConsole(80, 25)

	out.Reset()
	pf.enterHostConsole()

	// Scroll region for 25 lines with 2 overlay lines should be rows 1..23 (\x1b[1;23r)
	written := out.String()
	wantScrollRegion := "\x1b[1;23r"
	if !strings.Contains(written, wantScrollRegion) {
		t.Errorf("enterHostConsole with overlay missing scroll region %q: %q", wantScrollRegion, written)
	}

	out.Reset()
	pf.leaveHostConsole()

	// Leaving must restore scroll region (\x1b[r)
	written = out.String()
	if !strings.Contains(written, "\x1b[r") {
		t.Errorf("leaveHostConsole missing scroll region reset \\x1b[r: %q", written)
	}
}

func TestHostConsole_FarStylePTYSizing(t *testing.T) {
	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleOverlayUI = true

	pf := setupMockPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeHost
	pf.showKeyBar = true

	pf.ResizeConsole(80, 25)

	// PTY should receive height 25 - 2 = 23
	if pf.termView.Height != 23 {
		t.Errorf("termView height in Far-style host mode = %d, want 23", pf.termView.Height)
	}
}
