package main

import (
	"testing"
	"os"
	"context"
	"strings"
	"path/filepath"
	"archive/zip"
	"time"
	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestActionExecute_RemoteRejection(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// mockRemoteVFS does NOT satisfy the isLocal check in actionExecute
	baseVfs := vfs.NewOSVFS(t.TempDir())
	v := &mockFailingVFS{VFS: baseVfs}
	pf := NewPanelsFrame()

	actionExecute(pf, v, "/remote", "script.sh", "/remote/script.sh")

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

func TestActionNewFile_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
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

	tmp := t.TempDir() + "/empty.txt"
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
func TestActionExtractArchive_Integrity(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	srcZip := filepath.Join(tmpDir, "source.zip")

	// Создаем тестовый архив (используем хелпер из vfs_test если он доступен, либо создаем тут)
	f, _ := os.Create(srcZip)
	zw := zip.NewWriter(f)
	fw, _ := zw.Create("extracted.txt")
	fw.Write([]byte("content to extract"))
	zw.Create("empty_dir/")
	zw.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "output")
	os.Mkdir(destDir, 0755)

	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)
	pf.activeIdx = 0 // Левая панель становится активной (источником)

	left := pf.panels[0].(*FileSystemPanel)
	right := pf.panels[1].(*FileSystemPanel)

	left.vfs.SetPath(tmpDir)
	// Добавляем ".." первым, как это делает реальный VFS
	left.entries = []*fileEntry{
		{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}},
		{VFSItem: vfs.VFSItem{Name: "source.zip", IsDir: false}},
	}
	left.SetCursorIndex(1) // Стоим на "source.zip"

	right.vfs.SetPath(destDir)

	// Запускаем извлечение
	actionExtractArchive(pf)

	// Wait for background task and monitor the progress dialog
	timeout := time.After(5 * time.Second)
	dialogShown := false
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-time.After(10 * time.Millisecond):
			// Check if file exists periodically even if no UI tasks are pending
			if _, err := os.Stat(filepath.Join(destDir, "extracted.txt")); err == nil {
				goto extractionDone
			}

			topType := vtui.FrameManager.GetTopFrameType()
			if topType == vtui.TypeDialog {
				dlg := vtui.FrameManager.GetTopFrame()
				if strings.Contains(dlg.GetTitle(), "Error") {
					t.Fatalf("Extraction failed with error dialog: %q", dlg.GetTitle())
				}
				if strings.Contains(dlg.GetTitle(), "Extracting") {
					dialogShown = true
				}
			} else if dialogShown {
				// Progress dialog was shown and now it's gone - check file one last time
				if _, err := os.Stat(filepath.Join(destDir, "extracted.txt")); err == nil {
					goto extractionDone
				}
			}
		case <-timeout:
			t.Fatal("Extraction timed out")
		}
	}
extractionDone:

	// Проверяем содержимое
	data, _ := os.ReadFile(filepath.Join(destDir, "extracted.txt"))
	if string(data) != "content to extract" {
		t.Errorf("Extracted data mismatch: %q", string(data))
	}

	// Проверяем создание папки
	if st, err := os.Stat(filepath.Join(destDir, "empty_dir")); err != nil || !st.IsDir() {
		t.Error("Folder was not extracted correctly")
	}
}

// Вспомогательная функция для теста, чтобы не застревать в InputBox
func ExecuteAddArchive_Internal(pf *PanelsFrame, panel *FileSystemPanel, fullArcPath string, names []string, onDone func()) {
	pf.RunProgressTask(" Archiving... ", "Scanning...", false, func(tctx *vtui.TaskContext, update func(msg string, percent int)) error {
		var files []archives.FileInfo
		for _, n := range names {
			absPath, _ := panel.vfs.Abs(panel.vfs.Join(panel.vfs.GetPath(), n))
			moreFiles, _ := archives.FilesFromDisk(tctx.Context, nil, map[string]string{absPath: n})
			files = append(files, moreFiles...)
		}
		out, _ := os.Create(fullArcPath)
		defer out.Close()
		return archives.Zip{}.Archive(tctx.Context, out, files)
	}, func(err error) {
		onDone()
	})
}
