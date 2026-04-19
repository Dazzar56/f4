package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// walkUI is a local helper to find elements in nested containers
func walkUI(el vtui.UIElement, fn func(vtui.UIElement) bool) bool {
	if !fn(el) {
		return false
	}
	if c, ok := el.(vtui.Container); ok {
		for _, child := range c.GetChildren() {
			if !walkUI(child, fn) {
				return false
			}
		}
	}
	return true
}

func TestAttributesDialog_Layout(t *testing.T) {
	vtui.SetDefaultPalette()
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	v := vfs.NewOSVFS(".")
	item := vfs.VFSItem{Name: "test.txt", Uid: 1000, Gid: 1000, UnixMode: 0644}

	// We test only Unix layout in this env, but it proves the engine works
	showAttributesUnix(nil, v, "test.txt", item)

	top := vtui.FrameManager.GetTopFrame()
	dlg, ok := top.(vtui.Container)
	if !ok {
		t.Fatal("Dialog not found on top")
	}

	vtui.AssertLayout(t, dlg)
	top.SetExitCode(-1)
	vtui.FrameManager.Pop()
}

func TestAttributesDialog_UnixSync(t *testing.T) {
	vtui.SetDefaultPalette()
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	v := vfs.NewOSVFS(".")
	item := vfs.VFSItem{Name: "test.txt", UnixMode: 0644} // rw-r--r--

	showAttributesUnix(nil, v, "test.txt", item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var editOct *vtui.Edit
	var checkUserRead *vtui.Checkbox

	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if c, ok := el.(*vtui.Checkbox); ok && strings.Contains(c.GetText(), "Read") && checkUserRead == nil {
			checkUserRead = c
		}
		if e, ok := el.(*vtui.Edit); ok && e.GetText() == "0644" {
			editOct = e
		}
		return true
	})

	if editOct == nil || checkUserRead == nil {
		t.Fatal("Required UI elements for syncing test not found")
	}

	// 1. CheckBox -> Edit Sync
	// Uncheck 'Read' for user (bit 0400)
	checkUserRead.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_SPACE})

	// 0644 - 0400 = 0244
	if editOct.GetText() != "0244" {
		t.Errorf("Checkbox to Octal sync failed. Expected '0244', got %q", editOct.GetText())
	}

	// 2. Edit -> CheckBox Sync
	// Set mode to 0777 (all checked)
	editOct.SetText("0777")
	if editOct.OnTextChange != nil {
		editOct.OnTextChange("0777")
	}

	if checkUserRead.State != 1 {
		t.Error("Octal to Checkbox sync failed: Read box should be checked for 0777")
	}

	fm.GetTopFrame().SetExitCode(-1)
	fm.Pop()
}

func TestAttributesDialog_Validation(t *testing.T) {
	vtui.SetDefaultPalette()
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())
	v := vfs.NewOSVFS(".")
	item := vfs.VFSItem{Name: "test", UnixMode: 0644}

	showAttributesUnix(nil, v, "test", item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var editOct *vtui.Edit
	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if e, ok := el.(*vtui.Edit); ok {
			// Octal field is the only one with OctalValidator
			if _, ok := e.Validator.(*vtui.OctalValidator); ok {
				editOct = e
				return false
			}
		}
		return true
	})

	if editOct == nil {
		t.Fatal("Octal edit field not found")
	}

	// Try to type invalid octal digit '8'
	oldText := editOct.GetText()
	// ProcessKey returns true because it handled (swallowed) the invalid character
	_ = editOct.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '8'})

	if editOct.GetText() != oldText {
		t.Errorf("Octal field accepted invalid digit '8'. Text is now %q", editOct.GetText())
	}

	fm.GetTopFrame().SetExitCode(-1)
	fm.Pop()
}

func TestAttributesDialog_SetFlow(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(path, []byte(""), 0644)

	baseVfs := vfs.NewOSVFS(tmpDir)
	var capturedItem vfs.VFSItem
	mock := &mockMetadataVFS{
		VFS:       baseVfs,
		onSetAttr: func(item vfs.VFSItem) { capturedItem = item },
	}

	item := vfs.VFSItem{Name: "file.txt", UnixMode: 0644, Uid: 10, Gid: 10}
	showAttributesUnix(nil, mock, path, item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var editOwner *vtui.Edit
	var btnSet *vtui.Button

	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if e, ok := el.(*vtui.Edit); ok {
			// Owner edit is at the top and doesn't have any validators attached
			if e.Validator == nil && editOwner == nil {
				editOwner = e
			}
		}
		if b, ok := el.(*vtui.Button); ok {
			clean, _, _ := vtui.ParseAmpersandString(b.GetText())
			if strings.Contains(clean, "Set") {
				btnSet = b
			}
		}
		return true
	})

	if editOwner == nil || btnSet == nil {
		t.Fatal("Required UI elements (Owner edit or Set button) not found")
	}

	// 1. Change values in UI
	editOwner.SetText("2000")

	// 2. Click Set
	if btnSet.OnClick != nil {
		btnSet.OnClick()
	}

	// 3. Pump tasks
	timeout := time.After(2 * time.Second)
Loop:
	for {
		top := fm.GetTopFrame()
		// CRITICAL: Exit when the dialog is marked Done. 
		// We don't wait for nil because cleanupDoneFrames only runs in the main loop.
		if top == nil || top.IsDone() {
			break Loop
		}
		select {
		case task := <-fm.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for Set task")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if capturedItem.Uid != 2000 {
		t.Errorf("Attribute update failed. Expected UID 2000, got %d", capturedItem.Uid)
	}
}

func TestAttributesDialog_WindowsLayout(t *testing.T) {
	vtui.SetDefaultPalette()
	fm := vtui.FrameManager
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	fm.Init(scr)

	v := vfs.NewOSVFS(".")
	item := vfs.VFSItem{Name: "winfile.exe", MTime: time.Now()}

	showAttributesWindows(nil, v, "winfile.exe", item)

	top := fm.GetTopFrame()
	dlg, ok := top.(vtui.Container)
	if !ok {
		t.Fatal("Windows attributes dialog not found")
	}

	vtui.AssertLayout(t, dlg)
	top.SetExitCode(-1)
	fm.Pop()
}