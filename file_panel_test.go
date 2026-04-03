package main

import (
	"os"
	"io"
	"path/filepath"
	"time"
	"sort"
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

func TestFileSystemPanel_NavigateUp_Selection(t *testing.T) {
	vtui.SetDefaultPalette()
	scr := vtui.NewScreenBuf()
	scr.Writer = io.Discard
	vtui.FrameManager.Init(scr)

	tmp := t.TempDir()
	sub := filepath.Join(tmp, "target_folder")
	os.Mkdir(sub, 0755)
	os.WriteFile(filepath.Join(tmp, "other.txt"), []byte(""), 0644)

	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS(sub))

	// Drain tasks to finish loading the initial directory
	timeout := time.After(1 * time.Second)
	for fp.isLoading {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for initial load")
		}
	}
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			goto done1
		}
	}
	done1:

	// Simulate pressing Enter on ".."
	fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
	fp.SetCursorIndex(0)
	fp.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})

	// Wait for the parent directory to finish loading
	timeout = time.After(1 * time.Second)
	for fp.isLoading {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for parent load")
		}
	}

	// Pump any remaining UI rendering/selection tasks
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			goto done2
		}
	}
	done2:

	// Ensure that after returning to the parent directory, the cursor is on the folder we just exited
	if fp.GetSelectedName() != "target_folder" {
		t.Errorf("Expected cursor to land on 'target_folder', got %q", fp.GetSelectedName())
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
func TestMediumRow_GetCellText(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 10, vfs.NewOSVFS("."))
	fp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "test.txt", IsDir: false}},
		{VFSItem: vfs.VFSItem{Name: "work", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
	}
	fp.SetViewMode(ViewModeMedium)

	mRow := &mediumRow{fp: fp, r: 0}

	if mRow.GetCellText(0) != "test.txt" { t.Errorf("Expected 'test.txt', got %q", mRow.GetCellText(0)) }
	if mRow.GetCellText(1) != "" { t.Errorf("Out of bounds should be empty") }

	fp.entries = make([]*fileEntry, 10)
	for i := 0; i < 10; i++ {
		fp.entries[i] = &fileEntry{VFSItem: vfs.VFSItem{Name: "f"}}
	}
	fp.entries[0].Name = "Left"
	fp.entries[7].Name = "Right"
	mRow = &mediumRow{fp: fp, r: 0}
	if mRow.GetCellText(0) != "Left" { t.Errorf("Expected 'Left', got %q", mRow.GetCellText(0)) }
	if mRow.GetCellText(1) != "Right" { t.Errorf("Expected 'Right', got %q", mRow.GetCellText(1)) }
}

func TestFileSystemPanel_CursorMapping(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 10, vfs.NewOSVFS("."))

	// Simulate 20 items manually so Refresh() doesn't wipe them
	fp.entries = make([]*fileEntry, 20)
	for i := range fp.entries {
		fp.entries[i] = &fileEntry{VFSItem: vfs.VFSItem{Name: "file"}}
	}

	// 1. Medium Mode (Column-Major)
	fp.SetViewMode(ViewModeMedium)
	fp.Refresh()
	fp.SetCursorIndex(3) // Index 3: Row 3, Col 0
	if fp.table.SelectPos != 3 || fp.table.SelectCol != 0 {
		t.Errorf("Medium mapping index 3: expected pos 3 col 0, got pos %d col %d", fp.table.SelectPos, fp.table.SelectCol)
	}

	fp.SetCursorIndex(10) // Index 10 with H=7 -> Col 1, Row 3
	if fp.table.SelectPos != 3 || fp.table.SelectCol != 1 {
		t.Errorf("Medium mapping index 10: expected pos 3 col 1, got pos %d col %d", fp.table.SelectPos, fp.table.SelectCol)
	}

	// 2. Detailed Mode
	fp.SetViewMode(ViewModeDetailed)
	fp.Refresh()
	fp.SetCursorIndex(5)
	if fp.table.SelectPos != 5 || fp.table.SelectCol != 0 {
		t.Errorf("Detailed mapping failed: expected pos 5, got %d", fp.table.SelectPos)
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
	// 1. Setup real TempDir with files
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file1.txt"), []byte("1"), 0644)
	os.WriteFile(filepath.Join(tmp, "file2.txt"), []byte("2"), 0644)
	os.WriteFile(filepath.Join(tmp, "file3.txt"), []byte("3"), 0644)

	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS(tmp))
	fp.viewMode = ViewModeDetailed

	// Bypass async ReadDirectory for precise testing
	fp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "file1.txt"}},
		{VFSItem: vfs.VFSItem{Name: "file2.txt"}},
		{VFSItem: vfs.VFSItem{Name: "file3.txt"}},
	}
	fp.Refresh()

	// 2. Select file1.txt (Index 1)
	fp.SetCursorIndex(1)
	fp.Refresh()

	// Press Insert
	fp.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_INSERT})

	if !fp.entries[1].Selected {
		t.Error("file1.txt (index 1) should be selected after Insert")
	}

	// Cursor should move to file2.txt (Index 2)
	if fp.GetCursorIndex() != 2 {
		t.Errorf("Cursor should move to 2, got %d", fp.GetCursorIndex())
	}

	// 3. Select file2.txt via Shift+Down
	fp.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN, ControlKeyState: vtinput.ShiftPressed,
	})

	if !fp.entries[2].Selected {
		t.Error("file2.txt (index 2) should be selected after Shift+Down")
	}

	// 4. Verify results
	names := fp.GetSelectedNames()
	if len(names) != 2 || names[0] != "file1.txt" || names[1] != "file2.txt" {
		t.Errorf("GetSelectedNames returned wrong result: %v", names)
	}
}

func TestFileSystemPanel_ProcessMouse(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))
	fp.SetViewMode(ViewModeDetailed)

	fp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "f1"}},
		{VFSItem: vfs.VFSItem{Name: "f2"}},
	}
	fp.Refresh()

	// Left Click on f1 (Index 1). Table at Y=1, header is 1, so row 0 is Y=2, row 1 is Y=3.
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 5, MouseY: 3, ButtonState: vtinput.FromLeft1stButtonPressed,
	})

	if fp.GetCursorIndex() != 1 {
		t.Errorf("Expected cursorIdx 1, got %d", fp.GetCursorIndex())
	}

	// Right click on f2 (Index 2). Data row 2 is Y=4.
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 5, MouseY: 4, ButtonState: vtinput.RightmostButtonPressed,
	})

	if fp.GetCursorIndex() != 2 {
		t.Errorf("Expected cursorIdx 2, got %d", fp.GetCursorIndex())
	}
	if !fp.entries[2].Selected {
		t.Error("Right click selection failed")
	}

	// Right click again without button release (dragging simulation) - should NOT unselect
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 6, MouseY: 4, ButtonState: vtinput.RightmostButtonPressed,
	})

	if !fp.entries[2].Selected {
		t.Error("Right click drag shouldn't unselect the same item")
	}

	// Release button
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: false,
		MouseX: 6, MouseY: 4, ButtonState: 0,
	})

	// Click again - SHOULD unselect
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 6, MouseY: 4, ButtonState: vtinput.RightmostButtonPressed,
	})

	if fp.entries[2].Selected {
		t.Error("New right click should toggle selection")
	}
}

func TestFileSystemPanel_MouseClick_Edges(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))
	fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: ".."}}}
	fp.SetCursorIndex(0)

	// 1. Click on panel border (Y=0)
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 5, MouseY: 0, ButtonState: vtinput.FromLeft1stButtonPressed,
	})
	if fp.GetCursorIndex() != 0 {
		t.Errorf("Clicking on border should not change selection. Got %d", fp.GetCursorIndex())
	}

	// 2. Click on table header (Y=1)
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		MouseX: 5, MouseY: 1, ButtonState: vtinput.FromLeft1stButtonPressed,
	})
	if fp.GetCursorIndex() != 0 {
		t.Errorf("Clicking on header should not change selection. Got %d", fp.GetCursorIndex())
	}
}

func TestFileSystemPanel_RightClick_ResetOnRelease(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))
	fp.viewMode = ViewModeDetailed
	fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "f1"}}}

	// 1. Right click once -> Selects
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true, MouseX: 5, MouseY: 2, ButtonState: vtinput.RightmostButtonPressed,
	})
	if !fp.entries[0].Selected { t.Fatal("Should be selected") }

	// 2. Release button -> Resets tracker
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: false, MouseX: 5, MouseY: 2, ButtonState: 0,
	})

	// 3. Right click again -> Should toggle (Unselect) even though it's the same index
	fp.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true, MouseX: 5, MouseY: 2, ButtonState: vtinput.RightmostButtonPressed,
	})
	if fp.entries[0].Selected {
		t.Error("Item should have been unselected after button release and re-click")
	}
}
func TestFileSystemPanel_IncrementalInteraction(t *testing.T) {
	scr := vtui.NewScreenBuf()
	scr.Writer = io.Discard
	vtui.FrameManager.Init(scr)
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS(t.TempDir()))

	// Ensure we have '..' as initial state
	fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}

	// Симулируем прилет первого чанка
	chunk1 := []vfs.VFSItem{
		{Name: "file_A", IsDir: false},
		{Name: "file_Z", IsDir: false},
	}

	// Вручную вызываем логику обработки чанка (имитируя прилет из горутины)
	fp.entries = append(fp.entries, &fileEntry{VFSItem: chunk1[0]}, &fileEntry{VFSItem: chunk1[1]})
	fp.Refresh()

	// Пользователь выбирает file_Z (это индекс 2, так как 0 это "..")
	fp.SelectName("file_Z")
	if fp.GetSelectedName() != "file_Z" {
		t.Fatalf("Failed to select file_Z, got %s", fp.GetSelectedName())
	}

	// Симулируем прилет второго чанка с файлом, который встанет В НАЧАЛО списка после сортировки
	chunk2 := []vfs.VFSItem{
		{Name: "file_0_first", IsDir: false},
	}

	// Эмуляция PostTask для второго чанка:
	currentSelected := fp.GetSelectedName() // "file_Z"
	fp.entries = append(fp.entries, &fileEntry{VFSItem: chunk2[0]})
	sort.Slice(fp.entries, func(i, j int) bool {
		if fp.entries[i].Name == ".." { return true }
		if fp.entries[j].Name == ".." { return false }
		return fp.entries[i].Name < fp.entries[j].Name
	})
	fp.Refresh()
	fp.SelectName(currentSelected) // Удерживаем курсор

	// Проверяем: file_Z теперь должен быть на индексе 3, но курсор должен быть все еще на нем
	if fp.GetSelectedName() != "file_Z" {
		t.Errorf("Cursor jumped! Expected 'file_Z', got '%s'", fp.GetSelectedName())
	}

	// Проверяем, что индекс реально изменился (был 2, стал 3)
	if fp.GetCursorIndex() != 3 {
		t.Errorf("Index should have shifted to 3, got %d", fp.GetCursorIndex())
	}
}
func TestFileSystemPanel_GetSuccessorName(t *testing.T) {
	fp := &FileSystemPanel{}

	setupEntries := func(names ...string) {
		fp.cursorIdx = 0 // Reset state between cases
		fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
		for _, n := range names {
			fp.entries = append(fp.entries, &fileEntry{VFSItem: vfs.VFSItem{Name: n}})
		}
	}

	// Case 1: Single item in the middle. Focus on B. Successor should be C.
	setupEntries("A", "B", "C")
	fp.cursorIdx = 2 // B (Index 0 is .., 1 is A, 2 is B)
	if res := fp.GetSuccessorName(); res != "C" {
		t.Errorf("Case 1 failed: expected 'C', got %q", res)
	}

	// Case 2: Single item at the end. Focus on C. Successor should be B.
	fp.cursorIdx = 3 // C
	if res := fp.GetSuccessorName(); res != "B" {
		t.Errorf("Case 2 failed: expected 'B', got %q", res)
	}

	// Case 3: Multiple selected in the middle. Select A, B. Successor should be C.
	setupEntries("A", "B", "C", "D")
	fp.entries[1].Selected = true // A
	fp.entries[2].Selected = true // B
	if res := fp.GetSuccessorName(); res != "C" {
		t.Errorf("Case 3 failed: expected 'C', got %q", res)
	}

	// Case 4: Multiple selected at the end. Select C, D. Successor should be B.
	setupEntries("A", "B", "C", "D")
	fp.entries[3].Selected = true // C
	fp.entries[4].Selected = true // D
	if res := fp.GetSuccessorName(); res != "B" {
		t.Errorf("Case 4 failed: expected 'B', got %q", res)
	}

	// Case 5: Empty list (only .. exists)
	setupEntries()
	if res := fp.GetSuccessorName(); res != ".." {
		t.Errorf("Case 5 failed: expected '..', got %q", res)
	}
}
func TestFileSystemPanel_AsyncPendingSelection(t *testing.T) {
	scr := vtui.NewScreenBuf()
	scr.Writer = io.Discard
	vtui.FrameManager.Init(scr)
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewOSVFS("."))

	// Target: we want to select "target.txt" which will arrive in the second chunk
	fp.pendingSelection = "target.txt"
	fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
	fp.cursorIdx = 0

	// 1. Simulate First Chunk (doesn't contain our target)
	chunk1 := []vfs.VFSItem{{Name: "aaa.txt"}, {Name: "bbb.txt"}}

	// Replicating the logic from ReadDirectory's onChunk callback
	newEntries := make([]*fileEntry, len(chunk1))
	for i, item := range chunk1 { newEntries[i] = &fileEntry{VFSItem: item} }

	fp.entries = append(fp.entries, newEntries...)
	sort.Slice(fp.entries, func(i, j int) bool { return fp.entries[i].Name < fp.entries[j].Name })

	// Run snapping logic (simplified from file_panel.go)
	for i, entry := range fp.entries {
		if entry.Name == fp.pendingSelection {
			fp.SetCursorIndex(i)
			fp.pendingSelection = ""
			break
		}
	}

	if fp.pendingSelection == "" || fp.GetSelectedName() == "target.txt" {
		t.Error("Snapped prematurely to non-existent item")
	}

	// 2. Simulate Second Chunk (contains our target)
	chunk2 := []vfs.VFSItem{{Name: "target.txt"}, {Name: "zzz.txt"}}
	newEntries2 := make([]*fileEntry, len(chunk2))
	for i, item := range chunk2 { newEntries2[i] = &fileEntry{VFSItem: item} }

	fp.entries = append(fp.entries, newEntries2...)
	sort.Slice(fp.entries, func(i, j int) bool { return fp.entries[i].Name < fp.entries[j].Name })

	// Run snapping logic again
	for i, entry := range fp.entries {
		if entry.Name == fp.pendingSelection {
			fp.SetCursorIndex(i)
			fp.pendingSelection = ""
			break
		}
	}

	if fp.pendingSelection != "" {
		t.Error("Failed to clear pendingSelection after item arrived")
	}
	if fp.GetSelectedName() != "target.txt" {
		t.Errorf("Cursor failed to snap to 'target.txt'. Currently on: %q", fp.GetSelectedName())
	}
}
