package main

import (
	"testing"
	"time"

	"bytes"
	"strings"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestSimpleInline_CommandExecution(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)

	oldWait := waitForAnyKey
	waitForAnyKey = func() {}
	defer func() { waitForAnyKey = oldWait }()

	pf.runSimpleInlineCommand(t.TempDir(), "echo simple_inline_test")
}

func TestSimpleCaptured_CommandExecution(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleCaptured
	pf.ResizeConsole(80, 25)

	pf.runSimpleCapturedCommand(t.TempDir(), "echo simple_captured_test")
	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetType() != vtui.TypeDialog {
		t.Fatal("runSimpleCapturedCommand should open a captured output dialog")
	}
}

func TestSimpleInline_ToggleAndAnyKeyReturn(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleMode = ConsoleViewMc

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	// 1. Panel.Toggle hides panels in view-only primary screen
	RunAction("Panel.Toggle")
	if pf.showPanels {
		t.Fatal("Panel.Toggle should hide panels in SimpleInline mode")
	}

	// 2. Any key in SimpleInline with hidden panels returns to panels
	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    ' ',
	})
	if !pf.showPanels {
		t.Fatal("Any keypress while viewing primary screen in SimpleInline mode must restore panels")
	}
}

// TestSimpleInline_CtrlOKeyUpDoesNotRestorePanels reproduces a flicker seen
// under Wine: Ctrl+O's KeyDown correctly hides the panels via the hotkey
// dispatcher, but its trailing KeyUp event (delivered as a separate
// InputEvent, e.KeyDown == false) used to fall through to the "any key
// returns to panels" fallback below unfiltered, immediately undoing the
// toggle within the same keystroke.
func TestSimpleInline_CtrlOKeyUpDoesNotRestorePanels(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleMode = ConsoleViewMc

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	RunAction("Panel.Toggle")
	if pf.showPanels {
		t.Fatal("Panel.Toggle should hide panels in SimpleInline mode")
	}

	// The KeyUp of the same Ctrl+O press that triggered the toggle above.
	pressKey(pf, &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         false,
		VirtualKeyCode:  vtinput.VK_O,
		Char:            0x0F,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if pf.showPanels {
		t.Fatal("Ctrl+O's KeyUp event must not restore panels on its own")
	}

	// A genuine subsequent keypress should still restore panels as normal.
	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    ' ',
	})
	if !pf.showPanels {
		t.Fatal("A real keypress after Ctrl+O's KeyUp should still restore panels")
	}
}

func TestSimpleCaptured_ToggleShowsToast(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	// Drain any stale tasks left over from a previous test sharing the
	// global TaskChan/FrameManager, so we don't pick up someone else's
	// queued toast below.
drain:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			break drain
		}
	}

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleCaptured
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	RunAction("Panel.Toggle")
	if !pf.showPanels {
		t.Fatal("Panel.Toggle should not hide panels in SimpleCaptured mode")
	}

	// ShowToast is posted to the UI task queue; pump it like the main loop
	// would. FrameManager.currentToast is global and Init() does not clear
	// it, so a toast left over from an earlier test in this package may
	// still be unexpired: wait for the toast we expect rather than for the
	// first non-empty one.
	want := Msg("Terminal.NotAvailableInEnv")
	var toast string
	timeout := time.After(1 * time.Second)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if toast = vtui.FrameManager.GetActiveToast(); toast == want {
				break Loop
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for toast %q, last seen %q", want, toast)
		}
	}
}

// TestSimpleInline_FarStyleKeepsConsoleAndTypes covers the Ctrl+O screen users
// actually get under Wine: the console stays visible, the f4 command line is
// drawn on it, and typing edits that command line instead of throwing the user
// back to the panels.
func TestSimpleInline_FarStyleKeepsConsoleAndTypes(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	var out bytes.Buffer
	scr.Writer = &out
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	oldCfg := AppConfig
	defer func() { AppConfig = oldCfg }()
	AppConfig.ConsoleMode = ConsoleViewFar

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.showKeyBar = true
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	if got := pf.overlayLines(); got != 2 {
		t.Fatalf("overlayLines() in Far style with keybar = %d, want 2", got)
	}

	out.Reset()
	RunAction("Panel.Toggle")
	if pf.showPanels {
		t.Fatal("Panel.Toggle should hide panels in SimpleInline mode")
	}
	// Command line row of an 80x25 screen with a two line overlay is row 24.
	if written := out.String(); !strings.Contains(written, "\x1b[24;1H") {
		t.Errorf("entering the Far-style console must draw the overlay, got %q", written)
	}

	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    'd',
	})
	if pf.showPanels {
		t.Fatal("typing in the Far-style console must not restore panels")
	}
	if got := pf.cmdLine.Edit.GetText(); got != "d" {
		t.Errorf("typed character should reach the command line, got %q", got)
	}
}
