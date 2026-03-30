package main

import (
	"testing"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestFileEntry_GetCellText(t *testing.T) {
	// Mock entries
	file := &fileEntry{VFSItem: vfs.VFSItem{Name: "test.txt", Size: 1024, IsDir: false}}
	dir := &fileEntry{VFSItem: vfs.VFSItem{Name: "work", IsDir: true}}

	// 1. Column 0 (Name)
	if file.GetCellText(0) != "test.txt" {
		t.Errorf("File name mismatch: %s", file.GetCellText(0))
	}
	if dir.GetCellText(0) != "/work" {
		t.Errorf("Dir name mismatch: %s", dir.GetCellText(0))
	}

	// 2. Column 1 (Size)
	if file.GetCellText(1) != "1024" {
		t.Errorf("File size mismatch: %s", file.GetCellText(1))
	}

	// Regular directories should have an empty size column
	if dir.GetCellText(1) != "" {
		t.Errorf("Regular dir should have empty size column, got: %q", dir.GetCellText(1))
	}

	// Only ".." directory should have the UP-DIR placeholder
	upDir := &fileEntry{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}
	if upDir.GetCellText(1) != "UP-DIR" {
		t.Errorf("Parent dir (..) should have UP-DIR placeholder, got: %q", upDir.GetCellText(1))
	}
}
func TestFileSystemPanel_Initialization(t *testing.T) {
	// Verify that NewFileSystemPanel initializes with valid geometry to prevent collapsed panels
	x, y, w, h := 10, 5, 40, 20
	fp := NewFileSystemPanel(x, y, w, h, vfs.NewOSVFS("."))

	if fp.X1 != x || fp.Y1 != y || fp.X2 != x+w-1 || fp.Y2 != y+h-1 {
		t.Errorf("Panel coordinates not initialized correctly: got (%d,%d)-(%d,%d)", fp.X1, fp.Y1, fp.X2, fp.Y2)
	}

	// Internal table must match panel interior (excluding borders)
	tx1, ty1, tx2, ty2 := fp.table.GetPosition()
	if tx1 != x+1 || ty1 != y+1 || tx2 != x+w-2 || ty2 != y+h-2 {
		t.Errorf("Internal table coordinates mismatch: got (%d,%d)-(%d,%d)", tx1, ty1, tx2, ty2)
	}

	if fp.viewMode != ViewModeMedium {
		t.Errorf("Default view mode should be Medium, got %v", fp.viewMode)
	}

	if !fp.table.CellSelection {
		t.Error("Medium mode should have CellSelection enabled on the table")
	}
}
func TestMultiFileRow_GetCellText(t *testing.T) {
	file := &fileEntry{VFSItem: vfs.VFSItem{Name: "test.txt", IsDir: false}}
	dir := &fileEntry{VFSItem: vfs.VFSItem{Name: "work", IsDir: true}}
	upDir := &fileEntry{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}

	mRow := &multiFileRow{entries: []*fileEntry{file, dir}}
	mRow2 := &multiFileRow{entries: []*fileEntry{upDir}}

	if mRow.GetCellText(0) != "test.txt" { t.Errorf("Expected 'test.txt', got %q", mRow.GetCellText(0)) }
	if mRow.GetCellText(1) != "/work" { t.Errorf("Expected '/work', got %q", mRow.GetCellText(1)) }
	if mRow.GetCellText(2) != "" { t.Errorf("Out of bounds should be empty") }

	if mRow2.GetCellText(0) != ".." { t.Errorf("Expected '..', got %q", mRow2.GetCellText(0)) }
}

func TestFileSystemPanel_CursorMapping(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))

	// Simulate 5 items
	fp.entries = make([]*fileEntry, 5)

	// Medium Mode (2 cols)
	fp.SetViewMode(ViewModeMedium)

	// Test mapping logic
	fp.SetCursorIndex(3)
	if fp.table.SelectPos != 1 || fp.table.SelectCol != 1 {
		t.Errorf("Medium mode mapping failed for index 3: pos=%d, col=%d", fp.table.SelectPos, fp.table.SelectCol)
	}
	if fp.GetCursorIndex() != 3 {
		t.Errorf("GetCursorIndex reverse mapping failed: got %d", fp.GetCursorIndex())
	}

	fp.SetCursorIndex(4) // Last item on odd count
	if fp.table.SelectPos != 2 || fp.table.SelectCol != 0 {
		t.Errorf("Medium mode mapping failed for index 4: pos=%d, col=%d", fp.table.SelectPos, fp.table.SelectCol)
	}

	// Detailed Mode (1 col)
	fp.SetViewMode(ViewModeDetailed)
	fp.SetCursorIndex(3)
	if fp.table.SelectPos != 3 {
		t.Errorf("Detailed mode mapping failed for index 3: pos=%d", fp.table.SelectPos)
	}
}

func TestFileSystemPanel_SelectName(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))
	fp.SetViewMode(ViewModeDetailed)

	// Mock entries
	fp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "a_folder", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "z_folder", IsDir: true}},
	}

	fp.SelectName("z_folder")

	if fp.table.SelectPos != 2 {
		t.Errorf("SelectName failed: expected index 2, got %d", fp.table.SelectPos)
	}

	// Should not change position if name not found
	fp.SelectName("non_existent")
	if fp.table.SelectPos != 2 {
		t.Errorf("SelectName should not change position on failure, got %d", fp.table.SelectPos)
	}
}

func TestFileSystemPanel_MultiSelect(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))
	fp.SetViewMode(ViewModeDetailed)

	// Mock entries
	fp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "file1.txt", IsDir: false}},
		{VFSItem: vfs.VFSItem{Name: "file2.txt", IsDir: false}},
		{VFSItem: vfs.VFSItem{Name: "file3.txt", IsDir: false}},
	}
	fp.table.SetRows([]vtui.TableRow{fp.entries[0], fp.entries[1], fp.entries[2], fp.entries[3]})

	fp.SetCursorIndex(1) // On file1.txt

	// Press Insert
	fp.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_INSERT})

	// Check if selected
	if !fp.entries[1].Selected {
		t.Error("file1.txt should be selected after Insert")
	}

	// Cursor should move to file2.txt
	if fp.GetCursorIndex() != 2 {
		t.Errorf("Cursor should move to 2, got %d", fp.GetCursorIndex())
	}

	// Press Shift+Down
	fp.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN, ControlKeyState: vtinput.ShiftPressed,
	})

	if !fp.entries[2].Selected {
		t.Error("file2.txt should be selected after Shift+Down")
	}
	if fp.GetCursorIndex() != 3 {
		t.Errorf("Cursor should move to 3, got %d", fp.GetCursorIndex())
	}

	// GetSelectedNames
	names := fp.GetSelectedNames()
	if len(names) != 2 || names[0] != "file1.txt" || names[1] != "file2.txt" {
		t.Errorf("GetSelectedNames returned wrong result: %v", names)
	}
}
