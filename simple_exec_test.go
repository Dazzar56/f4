package main

import (
	"testing"
	"time"

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

func TestSimpleCaptured_ToggleShowsToast(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleCaptured
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	RunAction("Panel.Toggle")
	if !pf.showPanels {
		t.Fatal("Panel.Toggle should not hide panels in SimpleCaptured mode")
	}

	// ShowToast is posted to the UI task queue; pump it like the main loop would.
	var toast string
	timeout := time.After(1 * time.Second)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if toast = vtui.FrameManager.GetActiveToast(); toast != "" {
				break Loop
			}
		case <-timeout:
			t.Fatal("Timeout waiting for toast")
		}
	}
	if toast != Msg("Terminal.NotAvailableInEnv") {
		t.Errorf("Expected toast %q, got %q", Msg("Terminal.NotAvailableInEnv"), toast)
	}
}
