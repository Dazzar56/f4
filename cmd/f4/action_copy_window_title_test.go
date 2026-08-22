package main

import (
	"testing"
	"time"

	"github.com/unxed/vtui"
)

func waitForWindowTitleClipboard(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := vtui.GetClipboard(); got == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return vtui.GetClipboard()
}

func TestAction_AppCopyWindowTitle(t *testing.T) {
	action, ok := GetAction("App.CopyWindowTitle")
	if !ok {
		t.Fatal("App.CopyWindowTitle is not registered")
	}
	if action.Area != "Common" || len(action.DefaultKeys) != 1 || action.DefaultKeys[0] != "CtrlAltShiftT" {
		t.Fatalf("action metadata = %+v", action)
	}

	origTemplate := AppConfig.ConsoleTitleTemplate
	defer func() { AppConfig.ConsoleTitleTemplate = origTemplate }()
	t.Cleanup(swapFrameManager(t))

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	vtui.FrameManager.Push(vtui.NewDesktop())
	vtui.SetClipboard("")
	if !RunAction("App.CopyWindowTitle") {
		t.Fatal("App.CopyWindowTitle did not run")
	}
	if got := waitForWindowTitleClipboard(t, "Desktop"); got != "Desktop" {
		t.Fatalf("clipboard = %q, want %q", got, "Desktop")
	}

	vtui.FrameManager.Push(vtui.NewCenteredDialog(40, 10, " User Menu "))
	vtui.SetClipboard("")
	if !RunAction("App.CopyWindowTitle") {
		t.Fatal("App.CopyWindowTitle did not run for dialog")
	}
	if got := waitForWindowTitleClipboard(t, "User Menu"); got != "User Menu" {
		t.Fatalf("dialog clipboard = %q, want %q", got, "User Menu")
	}
}
