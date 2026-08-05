package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// waitForHistoryClipboard mirrors waitForMarkedClipboard: SetClipboard runs
// off the UI goroutine, so we poll for a short deadline.
func waitForHistoryClipboard(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := vtui.GetClipboard(); got == want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return vtui.GetClipboard()
}

// TestHistoryHint_MessagesResolved guards against a typo in the language
// bundle keys — if the message isn't loaded, vtui.Msg returns "{key}" and
// the bottom border of the Alt+F8/Alt+F12 dialogs would show a placeholder.
func TestHistoryHint_MessagesResolved(t *testing.T) {
	for _, key := range []string{"History.CommandsHint", "History.FoldersHint"} {
		s := Msg(key)
		if s == "" || strings.HasPrefix(s, "{") {
			t.Errorf("Msg(%q) not resolved: %q", key, s)
			continue
		}
		// Each hint must at least reference Enter (paste/goto) and
		// Shift+Del (delete), the two shortcuts issue #290 called out as
		// undocumented.
		if !strings.Contains(s, "Enter") {
			t.Errorf("Msg(%q) missing Enter hint: %q", key, s)
		}
		if !strings.Contains(s, "Shift+Del") {
			t.Errorf("Msg(%q) missing Shift+Del hint: %q", key, s)
		}
	}
}

// TestActionCommandHistory_WiresHint verifies actionCommandHistory installs
// a historySearch with Msg("History.CommandsHint") — the string the F8
// dialog actually paints on its bottom border.
func TestActionCommandHistory_WiresHint(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := setupMockPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(120, 40)
	pf.cmdLine.Edit.History = []string{"ls -la", "grep -r foo ."}

	activeHistorySearch = nil
	t.Cleanup(func() {
		if activeHistorySearch != nil {
			activeHistorySearch.cleanup()
		}
	})

	actionCommandHistory(pf)

	if activeHistorySearch == nil {
		t.Fatal("actionCommandHistory did not install a historySearch")
	}
	want := Msg("History.CommandsHint")
	if got := activeHistorySearch.hint; got != want {
		t.Errorf("commands hint = %q, want %q", got, want)
	}
}

func TestActionFoldersHistory_WiresHint(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := setupMockPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(120, 40)

	// actionFoldersHistory uses GlobalHistoryProvider — seed it with an in-memory
	// stub so the dialog has something to render.
	prev := vtui.GlobalHistoryProvider
	vtui.GlobalHistoryProvider = &stubHistoryProvider{"folders": {"/tmp", "/home"}}
	t.Cleanup(func() { vtui.GlobalHistoryProvider = prev })

	activeHistorySearch = nil
	t.Cleanup(func() {
		if activeHistorySearch != nil {
			activeHistorySearch.cleanup()
		}
	})

	actionFoldersHistory(pf)

	if activeHistorySearch == nil {
		t.Fatal("actionFoldersHistory did not install a historySearch")
	}
	want := Msg("History.FoldersHint")
	if got := activeHistorySearch.hint; got != want {
		t.Errorf("folders hint = %q, want %q", got, want)
	}
}

type stubHistoryProvider map[string][]string

func (s stubHistoryProvider) LoadHistory(name string) []string { return s[name] }
func (s stubHistoryProvider) SaveHistory(name string, h []string) {
	dup := append([]string(nil), h...)
	s[name] = dup
}

// TestActionCommandHistory_CtrlIns_CopiesToClipboard exercises the far2l
// Ctrl+Ins shortcut we added: copy the highlighted entry to the system
// clipboard. Same VMenu key path also handles Ctrl+C.
func TestActionCommandHistory_CtrlIns_CopiesToClipboard(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	pf.cmdLine.Edit.History = []string{"echo one", "echo two", "echo three"}
	actionCommandHistory(pf)
	menu := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)
	// applyFilter puts newest at bottom → select "echo two" (middle).
	menu.SetSelectPos(1)
	vtui.SetClipboard("")

	menu.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_INSERT,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if got := waitForHistoryClipboard(t, "echo two"); got != "echo two" {
		t.Errorf("clipboard = %q, want %q", got, "echo two")
	}

	menu.SetExitCode(-1)
	vtui.FrameManager.Pop()
}

// TestActionCommandHistory_Del_ClearsAllAfterConfirm covers the far2l Del
// shortcut: prompt, then wipe the whole history and close the menu.
func TestActionCommandHistory_Del_ClearsAllAfterConfirm(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	prev := vtui.GlobalHistoryProvider
	saved := stubHistoryProvider{}
	vtui.GlobalHistoryProvider = &saved
	t.Cleanup(func() { vtui.GlobalHistoryProvider = prev })

	pf.cmdLine.Edit.History = []string{"cmd1", "cmd2"}
	actionCommandHistory(pf)
	menu := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)

	menu.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_DELETE,
	})
	// The confirm dialog is now on top; click Ok (button index 0).
	confirm, ok := vtui.FrameManager.GetTopFrame().(*vtui.Window)
	if !ok || confirm.OnResult == nil {
		t.Fatalf("expected confirmation dialog, got %T", vtui.FrameManager.GetTopFrame())
	}
	confirm.OnResult(0)

	if len(pf.cmdLine.Edit.History) != 0 {
		t.Errorf("history not cleared: %v", pf.cmdLine.Edit.History)
	}
	if len(saved["cmdline"]) != 0 {
		t.Errorf("provider not wiped: %v", saved["cmdline"])
	}
}

// TestActionCommandHistory_Del_CancelKeepsHistory guards against the same
// path silently wiping the history when the user says Cancel.
func TestActionCommandHistory_Del_CancelKeepsHistory(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	prev := vtui.GlobalHistoryProvider
	saved := stubHistoryProvider{"cmdline": {"cmd1", "cmd2"}}
	vtui.GlobalHistoryProvider = &saved
	t.Cleanup(func() { vtui.GlobalHistoryProvider = prev })

	pf.cmdLine.Edit.History = []string{"cmd1", "cmd2"}
	actionCommandHistory(pf)
	menu := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)

	menu.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_DELETE,
	})
	confirm := vtui.FrameManager.GetTopFrame().(*vtui.Window)
	confirm.OnResult(1) // Cancel

	if len(pf.cmdLine.Edit.History) != 2 {
		t.Errorf("history unexpectedly changed: %v", pf.cmdLine.Edit.History)
	}
}

// TestActionFoldersHistory_CtrlR_DropsMissingPaths exercises the far2l
// Ctrl+R shortcut on the folder-history dialog: prompt, then keep only
// paths that still exist on disk.
func TestActionFoldersHistory_CtrlR_DropsMissingPaths(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	real1 := t.TempDir()
	real2 := t.TempDir()
	missing := filepath.Join(real2, "no-such-dir")

	prev := vtui.GlobalHistoryProvider
	saved := stubHistoryProvider{"folders": {real1, missing, real2}}
	vtui.GlobalHistoryProvider = &saved
	t.Cleanup(func() { vtui.GlobalHistoryProvider = prev })

	// Sanity-check the "missing" path really is missing before we assert.
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("test setup: %q unexpectedly exists", missing)
	}

	actionFoldersHistory(pf)
	menu := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)

	menu.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_R,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	confirm := vtui.FrameManager.GetTopFrame().(*vtui.Window)
	confirm.OnResult(0) // Ok

	got := saved["folders"]
	want := []string{real1, real2}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("folders after Ctrl+R: %v, want %v", got, want)
	}
}
