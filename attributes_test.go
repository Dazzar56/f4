package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)
// mockMetadataVFS allows intercepting Stat and SetAttributes for testing.
type mockMetadataVFS struct {
	vfs.VFS
	onSetAttr    func(vfs.VFSItem)
	statErr      error
	setAttrErr   error
	statToReturn vfs.VFSItem
}

func (m *mockMetadataVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	if m.statErr != nil {
		return vfs.VFSItem{}, m.statErr
	}
	if m.statToReturn.Name != "" {
		return m.statToReturn, nil
	}
	return m.VFS.Stat(ctx, path)
}

func (m *mockMetadataVFS) SetAttributes(ctx context.Context, path string, item vfs.VFSItem) error {
	if m.setAttrErr != nil {
		return m.setAttrErr
	}
	if m.onSetAttr != nil {
		m.onSetAttr(item)
	}
	return nil
}

func TestAttributesDialog_StatFailure(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	mockVFS := &mockMetadataVFS{
		VFS:     vfs.NewOSVFS(t.TempDir()),
		statErr: os.ErrPermission,
	}
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)
	fsp := pf.getActivePanel()
	fsp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "locked.txt"}}}
	fsp.vfs = mockVFS

	// This should trigger an async Stat call that fails
	actionFileAttributes(pf)

	// Pump tasks and wait for the error dialog
	timeout := time.After(1 * time.Second)
	foundDialog := false
Loop:
	for {
		select {
		case task := <-fm.TaskChan:
			task()
			if fm.GetTopFrameType() == vtui.TypeDialog && strings.Contains(fm.GetTopFrame().GetTitle(), "Error") {
				foundDialog = true
				break Loop
			}
		case <-timeout:
			break Loop
		}
	}

	if !foundDialog {
		t.Error("Expected an error dialog when initial Stat fails")
	}
}

func TestAttributesDialog_SetAttributesFailure(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	mockVFS := &mockMetadataVFS{
		VFS:        vfs.NewOSVFS(t.TempDir()),
		setAttrErr: os.ErrPermission,
	}

	item := vfs.VFSItem{Name: "file.txt"}
	showAttributesUnix(nil, mockVFS, "/file.txt", item)
	attrDlg := fm.GetTopFrame()

	var btnSet *vtui.Button
	walkUI(attrDlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if b, ok := el.(*vtui.Button); ok && strings.Contains(b.GetText(), "Set") {
			btnSet = b
			return false
		}
		return true
	})

	if btnSet.OnClick != nil {
		btnSet.OnClick()
	}

	timeout := time.After(1 * time.Second)
	errorDialogShown := false
Loop:
	for {
		select {
		case task := <-fm.TaskChan:
			task()
			// Check if an error dialog appeared ON TOP of the attributes dialog
			if fm.GetTopFrame() != attrDlg && fm.GetTopFrameType() == vtui.TypeDialog {
				errorDialogShown = true
				break Loop
			}
		case <-timeout:
			break Loop
		}
	}

	if !errorDialogShown {
		t.Error("Expected an error dialog when SetAttributes fails")
	}
	if attrDlg.IsDone() {
		t.Error("Attributes dialog should remain open after a SetAttributes failure")
	}
}

func TestAttributesDialog_UnixSetAll(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	var capturedItem vfs.VFSItem
	mockVFS := &mockMetadataVFS{
		VFS:       vfs.NewOSVFS(t.TempDir()),
		onSetAttr: func(item vfs.VFSItem) { capturedItem = item },
	}
	item := vfs.VFSItem{Name: "test.sh", Uid: 1000, Gid: 1000, UnixMode: 0644, MTime: time.Now()}

	showAttributesUnix(nil, mockVFS, "test.sh", item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var editOwner, editGroup, editOctal, editMTime *vtui.Edit
	var btnSet *vtui.Button

	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if e, ok := el.(*vtui.Edit); ok {
			if e.Validator != nil {
				editOctal = e
			} else if editOwner == nil {
				editOwner = e // First edit is owner
			} else if editGroup == nil {
				editGroup = e // Second is group
			} else {
				editMTime = e
			}
		}
		if b, ok := el.(*vtui.Button); ok && strings.Contains(b.GetText(), "Set") {
			btnSet = b
		}
		return true
	})

	// Change values
	newTime := "01.02.2030 10:20:30"
	editOwner.SetText("root")
	editGroup.SetText("wheel")
	editOctal.SetText("0755")
	editMTime.SetText(newTime)
	// Trigger octal -> checkbox sync
	editOctal.OnTextChange("0755")

	// Click "Set"
	if btnSet != nil && btnSet.OnClick != nil {
		btnSet.OnClick()
	}

	// Wait for async SetAttributes call to happen with a 2-second timeout.
	// We check UnixMode because it's initially 0 in capturedItem and becomes 0755 on success.
	deadline := time.Now().Add(2 * time.Second)
	for capturedItem.UnixMode == 0 && time.Now().Before(deadline) {
		select {
		case task := <-fm.TaskChan:
			task()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if capturedItem.UnixMode != 0755 {
		t.Errorf("Unix mode not set. Expected 0755, got %04o", capturedItem.UnixMode)
	}
	expectedTime, _ := time.Parse("02.01.2006 15:04:05", newTime)
	if !capturedItem.MTime.Equal(expectedTime) {
		t.Errorf("MTime not set. Expected %v, got %v", expectedTime, capturedItem.MTime)
	}
	// Verify that the VFS received the correct data from UI strings
	if capturedItem.UnixMode != 0755 {
		t.Errorf("Unix mode not set. Expected 0755, got %04o", capturedItem.UnixMode)
	}
}

func TestAttributesDialog_WindowsSetFlags(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	var capturedItem vfs.VFSItem
	mockVFS := &mockMetadataVFS{
		VFS:       vfs.NewOSVFS(t.TempDir()),
		onSetAttr: func(item vfs.VFSItem) { capturedItem = item },
	}

	item := vfs.VFSItem{Name: "win.exe", MTime: time.Now()}
	showAttributesWindows(nil, mockVFS, "win.exe", item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var chkRO, chkHidden *vtui.Checkbox
	var btnSet *vtui.Button

	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if c, ok := el.(*vtui.Checkbox); ok {
			if strings.Contains(c.GetText(), "Read only") { chkRO = c }
			if strings.Contains(c.GetText(), "Hidden") { chkHidden = c }
		}
		if b, ok := el.(*vtui.Button); ok && strings.Contains(b.GetText(), "Set") {
			btnSet = b
		}
		return true
	})

	// Toggle checkboxes
	chkRO.State = 1
	chkHidden.State = 1

	if btnSet.OnClick != nil {
		btnSet.OnClick()
	}

	// Wait for async SetAttributes call with a 2-second timeout
	deadline := time.Now().Add(2 * time.Second)
	for capturedItem.Name == "" && time.Now().Before(deadline) {
		select {
		case task := <-fm.TaskChan:
			task()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Verify the VFS call was made
	if capturedItem.Name == "" {
		t.Error("SetAttributes was not called after clicking Save in Windows attributes dialog")
	}
}

func TestAttributesDialog_InvalidTime(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewSilentScreenBuf())

	mockVFS := &mockMetadataVFS{VFS: vfs.NewOSVFS(t.TempDir())}
	item := vfs.VFSItem{Name: "file.txt", MTime: time.Now()}

	showAttributesUnix(nil, mockVFS, "file.txt", item)
	dlg := fm.GetTopFrame().(vtui.Container)

	var editMTime *vtui.Edit
	var btnSet *vtui.Button

	walkUI(dlg.(vtui.UIElement), func(el vtui.UIElement) bool {
		if e, ok := el.(*vtui.Edit); ok && strings.Contains(e.GetText(), ":") {
			editMTime = e
		}
		if b, ok := el.(*vtui.Button); ok && strings.Contains(b.GetText(), "Set") {
			btnSet = b
		}
		return true
	})

	// Enter completely broken date
	editMTime.SetText("99.99.9999 25:61:99")

	if btnSet.OnClick != nil {
		btnSet.OnClick()
	}

	// Since parsing fails, the dialog should remain open (IsDone == false)
	if fm.GetTopFrame().IsDone() {
		t.Error("Dialog should not close when date is invalid")
	}
}

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