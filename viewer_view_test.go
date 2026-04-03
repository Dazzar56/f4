package main

import (
	"os"
	"testing"
	"context"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestViewerView_NavigationAndEOF(t *testing.T) {
	vtui.SetDefaultPalette()
	tmpDir := t.TempDir()
	tmp := tmpDir + "/test.txt"
	os.WriteFile(tmp, []byte("L1\nL2\nL3\nL4\nL5"), 0644) // 5 lines total

	v := vfs.NewOSVFS(tmpDir)
	vv, err := NewViewerView(context.Background(), v, tmp)
	if err != nil {
		t.Fatal(err)
	}
	vv.SetPosition(0, 0, 10, 3) // Height 4 (Y:0..3). 1 line status, 3 lines content.

	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(11, 4)
	vtui.FrameManager.Init(scr)

	// 1. Initial Render (Triggers async fetch)
	vv.Show(scr)

	// Wait for background loader
	select {
	case task := <-vtui.FrameManager.TaskChan:
		task()
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for initial fetch")
	}

	// Re-render to populate lineOffsets
	vv.Show(scr)

	if vv.TopOffset != 0 {
		t.Errorf("Initial offset should be 0, got %d", vv.TopOffset)
	}
	if vv.eofVisible {
		t.Error("EOF should not be visible initially")
	}

	// 2. Scroll Down (should move to L2)
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	vv.Show(scr)
	if vv.TopOffset <= 0 {
		t.Errorf("Offset should increase after VK_DOWN, got %d", vv.TopOffset)
	}

	// 3. Jump to End (L3, L4, L5 visible)
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_END})

	// VK_END triggers FindLineStart which triggers another fetch
	timeout := time.After(1 * time.Second)
	for !vv.eofVisible {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			vv.Show(scr)
		case <-timeout:
			t.Fatal("Timeout waiting for EOF fetch")
		}
	}

	if !vv.eofVisible {
		t.Error("EOF should be visible after VK_END")
	}

	// 4. Try scrolling past EOF
	oldOffset := vv.TopOffset
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if vv.TopOffset != oldOffset {
		t.Errorf("VK_DOWN should be blocked when eofVisible is true. Offset changed from %d to %d", oldOffset, vv.TopOffset)
	}
}
func TestViewerView_MouseScrollbar(t *testing.T) {
	vtui.SetDefaultPalette()
	// Create a file with enough content to scroll
	content := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10\n" // 10 lines, 33 bytes (3 per line + 1 for last \n)
	tmpDir := t.TempDir()
	tmp := tmpDir + "/test_mouse.txt"
	os.WriteFile(tmp, []byte(content), 0644)

	v := vfs.NewOSVFS(tmpDir)
	vv, err := NewViewerView(context.Background(), v, tmp)
	if err != nil {
		t.Fatalf("Failed to create ViewerView: %v", err)
	}

	// Setup viewport: 11 columns (X=0..10), 5 rows (Y=0..4)
	// Top bar at Y=0. Content area Y=1..4 (4 lines).
	// Scrollbar at X=10, Y=1..4.
	vv.SetPosition(0, 0, 10, 4)

	// Create a dummy ScreenBuf to pass to Show() for initial rendering.
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(11, 5) // width 11 (0..10), height 5 (0..4)
	vtui.FrameManager.Init(scr)

	// IMPORTANT: Call Show initially to populate vv.lineOffsets and set vv.TopOffset.
	// Without this, the navigation logic in ProcessKey has no context.
	vv.Show(scr)

	// Wait for background loader
	select {
	case task := <-vtui.FrameManager.TaskChan:
		task()
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for scrollbar initial fetch")
	}
	vv.Show(scr)

	// Ensure we start at the top
	vv.TopOffset = 0

	// Check initial state, especially if TopOffset is correctly 0 and eofVisible is false.
	// With 10 lines and 4 content rows, we are definitely not at EOF.
	if vv.TopOffset != 0 {
		t.Errorf("Initial TopOffset expected 0, got %d", vv.TopOffset)
	}
	if vv.eofVisible {
		t.Error("Initial eofVisible expected false, got true")
	}

	// --- Test 1: Mouse wheel down ---
	oldOff := vv.TopOffset
	vv.ProcessMouse(&vtinput.InputEvent{Type: vtinput.MouseEventType, WheelDirection: -1})
	vv.Show(scr) // Re-render to update internal state (like vv.lineOffsets)

	if vv.TopOffset == oldOff {
		t.Error("Test 1: Mouse wheel down failed to increase TopOffset")
	}
	if vv.TopOffset != 3 { // Expected to move to start of L2 (offset 3)
		t.Errorf("Test 1: Expected TopOffset 3, got %d", vv.TopOffset)
	}

	// --- Test 2: Click on bottom arrow ---
	oldOff = vv.TopOffset // Should be 3
	vv.ProcessMouse(&vtinput.InputEvent{
		Type:        vtinput.MouseEventType,
		KeyDown:     true, // Important for click events
		ButtonState: vtinput.FromLeft1stButtonPressed,
		MouseX:      10, // Scrollbar X position
		MouseY:      4,  // Bottom arrow Y position (vv.Y2)
	})
	vv.Show(scr) // Re-render

	if vv.TopOffset == oldOff {
		t.Error("Test 2: Click on bottom arrow failed to increase TopOffset")
	}
	if vv.TopOffset != 6 { // Expected to move to start of L3 (offset 6)
		t.Errorf("Test 2: Expected TopOffset 6, got %d", vv.TopOffset)
	}

	// --- Test 3: Mouse wheel up ---
	oldOff = vv.TopOffset // Should be 6
	vv.ProcessMouse(&vtinput.InputEvent{Type: vtinput.MouseEventType, WheelDirection: 1})
	vv.Show(scr)

	if vv.TopOffset == oldOff {
		t.Error("Test 3: Mouse wheel up failed to decrease TopOffset")
	}
	if vv.TopOffset != 3 { // Expected to move to start of L2
		t.Errorf("Test 3: Expected TopOffset 3, got %d", vv.TopOffset)
	}

	// --- Test 4: Click on top arrow ---
	oldOff = vv.TopOffset // Should be 3
	vv.ProcessMouse(&vtinput.InputEvent{
		Type:        vtinput.MouseEventType,
		KeyDown:     true,
		ButtonState: vtinput.FromLeft1stButtonPressed,
		MouseX:      10, // Scrollbar X position
		MouseY:      1,  // Top arrow Y position (vv.Y1+1)
	})
	vv.Show(scr)

	if vv.TopOffset == oldOff {
		t.Error("Test 4: Click on top arrow failed to decrease TopOffset")
	}
	if vv.TopOffset != 0 { // Expected to move to start of L1
		t.Errorf("Test 4: Expected TopOffset 0, got %d", vv.TopOffset)
	}
}

func TestViewerBar_Content(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
	tmpDir := t.TempDir()
	tmp := tmpDir + "/bar_test.txt"
	os.WriteFile(tmp, []byte("Some content"), 0644)

	v := vfs.NewOSVFS(tmpDir)
	vv, _ := NewViewerView(context.Background(), v, tmp)
	vv.SetPosition(0, 0, 40, 10)

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(41, 11)

	vv.HexMode = true
	vv.topBar.Show(scr)

	// Проверяем, что в баре есть путь к файлу и режим "Hex"
	// Проверяем всю доступную ширину буфера (40 колонок)
	foundHex := false
	foundPath := false
	for x := 0; x <= 40; x++ {
		cell := scr.GetCell(x, 0)
		if cell.Char == 'H' { foundHex = true }
		if cell.Char == 'b' { foundPath = true } // часть "bar_test.txt"
	}

	if !foundHex { t.Error("ViewerBar did not display 'Hex' mode") }
	if !foundPath { t.Error("ViewerBar did not display file path") }
}
func TestViewerView_HandleClose(t *testing.T) {
	tmpDir := t.TempDir()
	tmp := tmpDir + "/close_test.txt"
	os.WriteFile(tmp, []byte("content"), 0644)
	v := vfs.NewOSVFS(tmpDir)
	vv, _ := NewViewerView(context.Background(), v, tmp)

	if vv.IsDone() {
		t.Fatal("Viewer should not be done initially")
	}

	// Send CmClose
	vv.HandleCommand(vtui.CmClose, nil)

	if !vv.IsDone() {
		t.Error("ViewerView failed to set IsDone after receiving CmClose")
	}
}
func TestViewerView_GetTitle(t *testing.T) {
	// Need to use an existing file for NewViewerView, or mock the backend.
	// For a simple title test, creating a temp file is easiest.
	tmpDir := t.TempDir()
	tmp := tmpDir + "/doc.txt"
	os.WriteFile(tmp, []byte(""), 0644)

	v := vfs.NewOSVFS(tmpDir)
	vv, err := NewViewerView(context.Background(), v, tmp)
	if err != nil {
		t.Fatal(err)
	}

	if vv.GetTitle() != "View: doc.txt" {
		t.Errorf("GetTitle failed: %s", vv.GetTitle())
	}
}
