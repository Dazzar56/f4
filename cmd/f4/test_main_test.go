package main

import (
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"os"
	"testing"
)

// pressKey dispatches a key through the production input path: the
// macro/hotkey filter first (action hotkeys are dispatched there), then
// the frame's own ProcessKey for widget-level keys. It ensures the
// global managers exist and the frame is the top frame.
func pressKey(f vtui.Frame, e *vtinput.InputEvent) bool {
	if vtui.FrameManager == nil || len(vtui.FrameManager.Screens) == 0 {
		vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	}
	if GlobalHotkeysMgr == nil {
		GlobalHotkeysMgr = NewHotkeyManager("")
	}
	if MacroMgr == nil {
		MacroMgr = NewMacroManager("")
	}
	inStack := false
	for _, s := range vtui.FrameManager.Screens {
		for _, fr := range s.Frames {
			if fr == f {
				inStack = true
				break
			}
		}
	}
	if !inStack {
		vtui.FrameManager.Push(f)
	}
	if MacroMgr.Filter(e) {
		return true
	}
	return f.ProcessKey(e)
}

// preserveActionRegistry keeps tests that register synthetic actions from
// leaking them into later tests or the next -count iteration.
func preserveActionRegistry(t *testing.T) {
	t.Helper()
	oldRegistry := actionRegistry
	oldOrder := actionOrder
	actionRegistry = make(map[string]Action, len(oldRegistry))
	for key, action := range oldRegistry {
		actionRegistry[key] = action
	}
	actionOrder = append([]string(nil), oldOrder...)
	t.Cleanup(func() {
		actionRegistry = oldRegistry
		actionOrder = oldOrder
	})
}

func TestMain(m *testing.M) {
	vfs.InitSudoClient("/usr/bin/f4", "")

	// Unit tests must never hand control to the user's desktop. Individual
	// tests that exercise these routes install per-dialog/per-frame recorders.
	defaultExternalUICommandRunner = func(string, []string, string) error { return nil }
	defaultNativePropertiesOpener = func(string) error { return nil }

	// Frames must not fork the user's shell during unit tests; the few
	// tests that exercise the PTY path construct one explicitly.
	spawnLocalShellPTY = false

	// The machine's clipboard is global, slow to reach (pbcopy/xclip) and
	// shared with whatever else the CI runner is doing; tests keep clipboard
	// traffic in vtui's process-local buffer instead, and skip the OSC 52
	// stdout fallback that used to spray base64 into the test logs. A test
	// that genuinely targets the OS clipboard switches the knob back off
	// for its own scope.
	vtui.SkipOSClipboard(true)
	vtui.DisableTerminalClipboard()

	tmpDir, err := os.MkdirTemp("", "f4-test-config-*")
	if err == nil {
		// XDG_CONFIG_HOME/APPDATA cover Linux and Windows; os.UserConfigDir
		// ignores both on darwin, so the seam is what actually isolates the
		// suite from the developer's real profile there.
		os.Setenv("XDG_CONFIG_HOME", tmpDir)
		os.Setenv("APPDATA", tmpDir)
		userConfigDir = func() (string, error) { return tmpDir, nil }
		resetConfigDirForTest()
	}

	result := m.Run()

	if err == nil {
		os.RemoveAll(tmpDir)
	}

	if result != 0 {
		// disabled for now
		//vtui.DumpLogsToFile("_failed_tests_f4.log")
	}
	os.Exit(result)
}
