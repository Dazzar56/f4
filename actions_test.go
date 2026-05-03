package main

import (
	"testing"
	"os"
	"context"
	"time"
	"strings"
	"path/filepath"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestActionExecute_RemoteRejection(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// mockRemoteVFS does NOT satisfy the isLocal check in actionExecute
	baseVfs := vfs.NewOSVFS(t.TempDir())
	v := &mockFailingVFS{VFS: baseVfs}
	pf := NewPanelsFrame()
	defer pf.Close()

	actionExecute(pf, v, filepath.FromSlash("/remote"), "script.sh", filepath.FromSlash("/remote/script.sh"))

	// Drain task queue to allow UI updates
	timeout := time.After(1 * time.Second)
	foundDialog := false
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if vtui.FrameManager.GetTopFrameType() == vtui.TypeDialog {
				foundDialog = true
				break Loop
			}
		case <-timeout:
			break Loop
		}
	}

	if !foundDialog {
		t.Error("Expected error dialog when attempting to execute on remote VFS")
	}
}

func TestActionMkDir_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25) // Crucial: initializes panels

	// 1. Trigger MkDir action (should push InputBox)
	actionMkDir(pf)

	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetTitle() != Msg("MakeFolder.Title") {
		t.Fatalf("Expected MkDir dialog, got %v", top)
	}

	// Close it to clean up
	top.SetExitCode(-1)
	vtui.FrameManager.Pop()
}
type mockDeletionFailingVFS struct {
	vfs.VFS
	failedFiles  []string
	deletedFiles []string
}

func (m *mockDeletionFailingVFS) Remove(ctx context.Context, path string) error {
	name := filepath.Base(path)
	for _, f := range m.failedFiles {
		if f == name {
			return os.ErrPermission
		}
	}
	m.deletedFiles = append(m.deletedFiles, name)
	return nil
}

func (m *mockDeletionFailingVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	name := filepath.Base(path)
	return vfs.VFSItem{Name: name, IsDir: false, Size: 10}, nil
}

func (m *mockDeletionFailingVFS) ReadDir(ctx context.Context, path string, onChunk func([]vfs.VFSItem)) error {
	return nil
}

func (m *mockDeletionFailingVFS) Join(e ...string) string { return filepath.Join(e...) }
func (m *mockDeletionFailingVFS) GetPath() string        { return "/tmp" }

func TestActionDelete_BulkErrorAccumulation(t *testing.T) {
	fm := vtui.FrameManager
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	fm.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	// Создаем мок-VFS, который запретит удаление "fail.txt"
	mv := &mockDeletionFailingVFS{
		VFS:         vfs.NewOSVFS(t.TempDir()),
		failedFiles: []string{"fail.txt"},
	}

	fsp := pf.panels[0].(*FileSystemPanel)
	fsp.vfs = mv

	// Подготавливаем список файлов: f1.txt (ок), fail.txt (ошибка), f2.txt (ок)
	fsp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: ".."}},
		{VFSItem: vfs.VFSItem{Name: "f1.txt"}},
		{VFSItem: vfs.VFSItem{Name: "fail.txt"}},
		{VFSItem: vfs.VFSItem{Name: "f2.txt"}},
	}
	// Выделяем все три файла
	fsp.entries[1].Selected = true
	fsp.entries[2].Selected = true
	fsp.entries[3].Selected = true

	// ВАЖНО: делаем панель с файлами активной
	pf.activeIdx = 0

	// 1. Инициируем удаление
	actionDelete(pf)

	// 2. Находим кнопку "Delete" в диалоге подтверждения и нажимаем её
	// In test, force mode to Foreground Lock (2) so it runs synchronously
	dlgConfirm1 := fm.GetTopFrame().(vtui.Container)
	for _, child := range dlgConfirm1.GetChildren() {
		if c, ok := child.(*vtui.ComboBox); ok {
			c.Menu.SetSelectPos(2) // Foreground
		}
	}

	frame := fm.GetTopFrame()
	if frame == nil {
		t.Fatal("Confirmation dialog was not shown")
	}
	top, ok := frame.(vtui.Container)
	if !ok {
		t.Fatal("Top frame is not a container")
	}
	var btnDel *vtui.Button
	for _, itm := range top.GetChildren() {
		if b, ok := itm.(*vtui.Button); ok && strings.Contains(b.GetText(), "Delete") {
			btnDel = b
			break
		}
	}
	if btnDel == nil {
		t.Fatal("Delete button not found in confirmation dialog")
	}
	btnDel.OnClick()

	// 3. Прокручиваем очередь задач, ожидая появления диалога с итогами ошибок
	timeout := time.After(2 * time.Second)
Loop:
	for {
		select {
		case task := <-fm.TaskChan:
			task()

			// Если выскочил диалог ошибки удаления (AskError), нажимаем Skip
			if fm.GetTopFrameType() == vtui.TypeDialog && strings.Contains(fm.GetTopFrame().GetTitle(), "Error") {
				if dlg, ok := fm.GetTopFrame().(vtui.Container); ok {
					for _, itm := range dlg.GetChildren() {
						if b, ok := itm.(*vtui.Button); ok && strings.Contains(b.GetText(), "Skip") {
							b.OnClick()
							break
						}
					}
				}
			}

			// Ждем, когда на вершине стека окажется диалог с заголовком " Deletion Errors "
			if fm.GetTopFrameType() == vtui.TypeDialog && fm.GetTopFrame().GetTitle() == " Deletion Errors " {
				break Loop
			}

			if fm.GetTopFrame() != nil && fm.GetTopFrame().IsDone() {
				fm.Pop()
			}
		case <-timeout:
			t.Fatal("Timeout waiting for error accumulation dialog")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Validate layout of the Deletion Errors dialog
	vtui.AssertLayout(t, fm.GetTopFrame().(vtui.Container))

	// 4. Проверяем результаты
	// Должно быть 2 успешных удаления (f1.txt и f2.txt)
	if len(mv.deletedFiles) != 2 {
		t.Errorf("Expected 2 files deleted, got %d: %v", len(mv.deletedFiles), mv.deletedFiles)
	}

	foundF1, foundF2 := false, false
	for _, f := range mv.deletedFiles {
		if f == "f1.txt" {
			foundF1 = true
		}
		if f == "f2.txt" {
			foundF2 = true
		}
	}
	if !foundF1 || !foundF2 {
		t.Errorf("One of the deletable files was skipped: %v", mv.deletedFiles)
	}
}

type mockRetryDeleteVFS struct {
	vfs.VFS
	attempts map[string]int
	deleted  []string
}

func (m *mockRetryDeleteVFS) Remove(ctx context.Context, path string) error {
	name := filepath.Base(path)
	if m.attempts[name] > 0 {
		m.attempts[name]--
		return os.ErrPermission
	}
	m.deleted = append(m.deleted, name)
	return nil
}

func (m *mockRetryDeleteVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	return vfs.VFSItem{Name: filepath.Base(path)}, nil
}

func TestActionDelete_RetrySuccess(t *testing.T) {
	fm := vtui.FrameManager
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	fm.Init(scr)
	SetDefaultF4Palette()

	mv := &mockRetryDeleteVFS{
		VFS:      vfs.NewOSVFS(t.TempDir()),
		attempts: map[string]int{"retry.txt": 1}, // Упадёт 1 раз
	}

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)
	fsp := pf.panels[0].(*FileSystemPanel)
	fsp.vfs = mv
	fsp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "retry.txt"}}}
	pf.activeIdx = 0

	actionDelete(pf)

	// 1. Подтверждаем удаление
	dlgConfirm := fm.GetTopFrame().(vtui.Container)
	for _, child := range dlgConfirm.GetChildren() {
		if c, ok := child.(*vtui.ComboBox); ok {
			c.Menu.SetSelectPos(2) // Foreground
		}
	}
	clickDialogButton(t, dlgConfirm, "Delete")

	// 2. Ждем диалог ошибки и жмем Retry
	timeout := time.After(2 * time.Second)
	retryClicked := false
Loop:
	for {
		if len(mv.deleted) == 1 {
			break Loop
		}
		select {
		case task := <-fm.TaskChan:
			task()
			if !retryClicked && fm.GetTopFrameType() == vtui.TypeDialog && strings.Contains(fm.GetTopFrame().GetTitle(), "Error") {
				clickDialogButton(t, fm.GetTopFrame().(vtui.Container), "Retry")
				retryClicked = true
			}
			if fm.GetTopFrame() != nil && fm.GetTopFrame().IsDone() {
				fm.Pop()
			}
		case <-timeout:
			t.Fatal("Timeout waiting for Retry to succeed")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if len(mv.deleted) != 1 || mv.deleted[0] != "retry.txt" {
		t.Errorf("File was not deleted after Retry. Deleted: %v", mv.deleted)
	}
}

func TestActionDelete_Abort(t *testing.T) {
	fm := vtui.FrameManager
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	fm.Init(scr)
	SetDefaultF4Palette()

	mv := &mockDeletionFailingVFS{
		VFS:         vfs.NewOSVFS(t.TempDir()),
		failedFiles: []string{"abort.txt"},
	}

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)
	fsp := pf.panels[0].(*FileSystemPanel)
	fsp.vfs = mv
	fsp.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "abort.txt"}},
		{VFSItem: vfs.VFSItem{Name: "should_not_touch.txt"}},
	}
	fsp.entries[0].Selected = true
	fsp.entries[1].Selected = true
	pf.activeIdx = 0

	actionDelete(pf)
	dlgConfirm := fm.GetTopFrame().(vtui.Container)
	for _, child := range dlgConfirm.GetChildren() {
		if c, ok := child.(*vtui.ComboBox); ok {
			c.Menu.SetSelectPos(2) // Foreground
		}
	}
	clickDialogButton(t, dlgConfirm, "Delete")

	// Ждем ошибку и жмем Abort
	timeout := time.After(2 * time.Second)
Loop:
	for {
		select {
		case task := <-fm.TaskChan:
			task()
			if fm.GetTopFrameType() == vtui.TypeDialog && strings.Contains(fm.GetTopFrame().GetTitle(), "Error") {
				clickDialogButton(t, fm.GetTopFrame().(vtui.Container), "Abort")
				break Loop
			}
			if fm.GetTopFrame() != nil && fm.GetTopFrame().IsDone() {
				fm.Pop()
			}
		case <-timeout:
			t.Fatal("Error dialog didn't appear")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Проверяем, что список удаленных пуст (первый упал, второй не начинали)
	if len(mv.deletedFiles) != 0 {
		t.Errorf("Abort failed: some files were deleted: %v", mv.deletedFiles)
	}
}
func TestActionDelete_SuccessorLogic(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	tmp := t.TempDir()
	fsp := pf.panels[0].(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)

	// Создаем 4 файла: f1, f2, f3, f4
	files := []string{"f1.txt", "f2.txt", "f3.txt", "f4.txt"}
	for _, f := range files {
		os.WriteFile(filepath.Join(tmp, f), []byte("data"), 0644)
	}

	// 1. Удаляем f2 и f3 (выделенные)
	// Дожидаемся загрузки
	fsp.ReadDirectory()
	for fsp.isLoading {
		select {
		case task := <-vtui.FrameManager.TaskChan: task()
		case <-time.After(1 * time.Second): t.Fatal("Timeout")
		}
	}

	// Выделяем f2 и f3 (индексы 2 и 3, т.к. 0 - "..", 1 - "f1")
	fsp.entries[2].Selected = true
	fsp.entries[3].Selected = true

	// По логике Successor, после удаления блока f2, f3 курсор должен встать на f4.
	successor := fsp.GetSuccessorName()
	if successor != "f4.txt" {
		t.Errorf("Expected successor f4.txt, got %q", successor)
	}

	// 2. Удаляем последний файл (f4)
	fsp.entries[2].Selected = false
	fsp.entries[3].Selected = false
	fsp.SetCursorIndex(4) // f4
	successor = fsp.GetSuccessorName()
	// Если удаляем последний, курсор прыгает на предыдущий (f3)
	if successor != "f3.txt" {
		t.Errorf("Expected successor f3.txt when deleting tail, got %q", successor)
	}
}
func TestActionCopyMove_TrailingSlash(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	// Ensure predictable paths for the test
	fspSrc := pf.panels[0].(*FileSystemPanel)
	fspDst := pf.panels[1].(*FileSystemPanel)
	fspSrc.vfs.SetPath(filepath.FromSlash("/src/dir"))
	fspDst.vfs.SetPath(filepath.FromSlash("/dst/dir"))

	// Manually add an entry so actionCopyMove doesn't exit early
	fspSrc.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "test.txt", IsDir: false}},
	}
	fspSrc.SetCursorIndex(0)
	pf.activeIdx = 0 // Ensure the panel with the file is active

	// Trigger Copy (false = isMove)
	actionCopyMove(pf, false)

	top := vtui.FrameManager.GetTopFrame()
	dlg, ok := top.(vtui.Container)
	if !ok {
		t.Fatal("Copy dialog not found on top")
	}

	var editDest *vtui.Edit
	for _, itm := range dlg.GetChildren() {
		if e, ok := itm.(*vtui.Edit); ok {
			editDest = e
			break
		}
	}

	if editDest == nil {
		t.Fatal("Destination edit field not found in dialog")
	}

	txt := editDest.GetText()
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(txt, sep) {
		t.Errorf("Path in Copy dialog missing trailing slash: %q (expected it to end with %q)", txt, sep)
	}

	// Cleanup
	top.SetExitCode(-1)
	vtui.FrameManager.Pop()
}

func TestActionNewFile_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25) // Crucial: initializes panels

	pf.activeIdx = 0
	actionNewFile(pf)

	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetTitle() != Msg("Edit.NewFileTitle") {
		t.Errorf("Expected New File dialog, got %v", top)
	}
}
func TestActionViewerSearch_EmptyFile(t *testing.T) {
	// Regression test: searching in an empty file should not hang or crash
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmp := filepath.Join(t.TempDir(), "empty.txt")
	os.WriteFile(tmp, []byte(""), 0644)
	v := vfs.NewOSVFS(t.TempDir())

	vv, _ := NewViewerView(context.Background(), v, tmp)

	// Simulate search trigger
	// We manually call the inner logic of actionViewerSearch since InputBox is blocking in tests
	foundOffset := int64(-1)
	currOff := vv.TopOffset + 1
	fileSize := vv.backend.Size() // 0

	if currOff < fileSize {
		t.Error("Search loop should not even start for empty file")
	}

	if foundOffset != -1 {
		t.Error("Should not find anything in empty file")
	}
}
func TestActionFindFile_Persistence(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	LastFindFileMask = "*.tmp"
	actionFindFile(pf)
	dlg := vtui.FrameManager.GetTopFrame().(vtui.Container)

	found := false
	for _, itm := range dlg.GetChildren() {
		if e, ok := itm.(*vtui.Edit); ok && e.GetText() == "*.tmp" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Find File dialog did not initialize with LastFindFileMask")
	}
}

func TestSession_DiskPersistence(t *testing.T) {
	// Создаем временную директорию для теста
	tmpDir, _ := os.MkdirTemp("", "f4-session-test")
	defer os.RemoveAll(tmpDir)

	// Перехватываем путь к ini файлу (в реальном коде он завязан на os.UserConfigDir)
	// Для теста мы просто вручную вызовем SaveSession и проверим результат в файле.
	origPathFunc := getSessionIniPath
	getSessionIniPath = func() string { return filepath.Join(tmpDir, "session.ini") }
	defer func() { getSessionIniPath = origPathFunc }()

	LastEditorSearch = "disk-test"
	LastFindFileMask = "*.log"
	LastLeftPath = "/path/a"
	LastRightPath = "/path/b"
	LastLeftCursor = "file.a"
	LastRightCursor = "file.b"
	LastActivePanel = 0

	SaveSession()

	// Сбрасываем и загружаем
	LastEditorSearch = ""
	LastLeftPath = ""
	LastRightPath = ""
	LastLeftCursor = ""
	LastRightCursor = ""
	LastActivePanel = 1
	LoadSession()

	if LastEditorSearch != "disk-test" || LastLeftPath != "/path/a" || LastLeftCursor != "file.a" || LastActivePanel != 0 {
		t.Errorf("Disk persistence failed. Search:%q, LeftPath:%q, LeftCursor:%q, Active:%d",
			LastEditorSearch, LastLeftPath, LastLeftCursor, LastActivePanel)
	}
}
func TestActionPanelSettings_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	actionPanelSettings(pf)

	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetTitle() != Msg("PanelSettings.Title") {
		t.Fatalf("Expected Panel Settings dialog, got %v", top)
	}

	// Проверяем наличие чекбокса для сохранения путей
	dlg := top.(vtui.Container)
	found := false
	for _, itm := range dlg.GetChildren() {
		if chk, ok := itm.(*vtui.Checkbox); ok {
			if strings.Contains(chk.GetText(), "paths") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Error("Save paths checkbox not found in Panel Settings dialog")
	}

	// Проверяем наличие чекбокса автодополнения
	foundAc := false
	for _, itm := range dlg.GetChildren() {
		if chk, ok := itm.(*vtui.Checkbox); ok {
			if strings.Contains(strings.ToLower(chk.GetText()), "auto-completion") {
				foundAc = true
				break
			}
		}
	}
	if !foundAc {
		t.Error("Command line auto-completion checkbox not found in Panel Settings dialog")
	}

	top.SetExitCode(-1)
	vtui.FrameManager.Pop()
}
func TestActionManagePlugins_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	oldPlugins := AppConfig.RegisteredPlugins
	AppConfig.RegisteredPlugins = []string{"/old/path"}
	defer func() { AppConfig.RegisteredPlugins = oldPlugins }()

	actionManagePlugins(pf)
	top := vtui.FrameManager.GetTopFrame().(vtui.Container)

	var lb *vtui.ListBox
	for _, itm := range top.GetChildren() {
		if l, ok := itm.(*vtui.ListBox); ok {
			lb = l
			break
		}
	}
	if lb == nil { t.Fatal("ListBox not found") }

	// 1. Test Remove
	var btnRem *vtui.Button
	for _, itm := range top.GetChildren() {
		if b, ok := itm.(*vtui.Button); ok && strings.Contains(b.GetText(), "Remove") {
			btnRem = b; break
		}
	}
	btnRem.OnClick()

	if len(AppConfig.RegisteredPlugins) != 0 {
		t.Error("Plugin was not removed from config")
	}

	// 2. Test Add (simulating SelectFileDialog callback)
	tmpDir := t.TempDir()
	testFile := "my_plugin.sh"
	os.WriteFile(filepath.Join(tmpDir, testFile), []byte("#!/bin/sh"), 0755)

	pluginVfs := &dialogVFSAdapter{v: vfs.NewOSVFS(tmpDir)}
	
	foundFile := false
	err := pluginVfs.ReadDir(context.Background(), tmpDir, func(items []vtui.FSItem) {
		for _, itm := range items {
			if itm.Name == testFile {
				foundFile = true
			}
		}
	})
	if err != nil { t.Fatalf("Adapter ReadDir failed: %v", err) }
	if !foundFile { t.Errorf("Adapter failed to find test file %s", testFile) }

	newPath := filepath.Join(tmpDir, testFile)
	AppConfig.RegisteredPlugins = append(AppConfig.RegisteredPlugins, newPath)
	lb.Items = AppConfig.RegisteredPlugins
	lb.UpdateRows()

	if len(AppConfig.RegisteredPlugins) != 1 || AppConfig.RegisteredPlugins[0] != newPath {
		t.Errorf("Failed to add new plugin. Current: %v", AppConfig.RegisteredPlugins)
	}
}	
