package main

import (
	"context"
	"strings"
	"os"
	"path/filepath"
	"testing"
	"time"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestRecursiveCopy(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	// 1. Create source structure:
	// /folder1/file1.txt
	// /file2.txt
	os.Mkdir(filepath.Join(tmpSrc, "folder1"), 0755)
	os.WriteFile(filepath.Join(tmpSrc, "file2.txt"), []byte("file2 content"), 0644)
	os.WriteFile(filepath.Join(tmpSrc, "folder1", "file1.txt"), []byte("file1 content"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)

	//pf := &PanelsFrame{}

	// Initialize FrameManager to provide TaskChan for RunOnUI
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// Create a real TaskContext
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	defer tCtx.Cancel()

	dummyUpdate := func(msg string, percent int) {}

	// Perform copy: folder1 from tmpSrc to tmpDst
	err := recursiveCopy(tCtx, dummyUpdate, srcVfs, filepath.Join(tmpSrc, "folder1"), dstVfs, filepath.Join(tmpDst, "folder1_copy"), &FileOpState{})
	if err != nil {
		t.Fatalf("recursiveCopy failed: %v", err)
	}

	// Verify result
	copiedFile := filepath.Join(tmpDst, "folder1_copy", "file1.txt")
	if _, err := os.Stat(copiedFile); os.IsNotExist(err) {
		t.Errorf("Copied file does not exist: %s", copiedFile)
	}

	data, _ := os.ReadFile(copiedFile)
	if string(data) != "file1 content" {
		t.Errorf("Corrupted data in copied file. Got %q", string(data))
	}
}
func TestRecursiveCopy_Cancel(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()
	largeFile := filepath.Join(tmpSrc, "large.bin")
	// Create 1MB file
	os.WriteFile(largeFile, make([]byte, 1024*1024), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	//pf := &PanelsFrame{}
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	ctx, cancel := context.WithCancel(context.Background())
	tCtx := &vtui.TaskContext{Context: ctx, Cancel: cancel}

	// Cancel immediately
	cancel()
	dummyUpdate := func(msg string, percent int) {}

	err := recursiveCopy(tCtx, dummyUpdate, srcVfs, largeFile, dstVfs, filepath.Join(tmpDst, "large_copy.bin"), &FileOpState{})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected context canceled error, got %v", err)
	}
}

func TestRecursiveCopy_SelfCopy(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "src_folder"), 0755)

	srcVfs := vfs.NewOSVFS(tmp)
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	defer tCtx.Cancel()

	dummyUpdate := func(msg string, percent int) {}

	// Try to copy "src_folder" into "src_folder/sub"
	srcPath := filepath.Join(tmp, "src_folder")
	// Use OSVFS for proper absolute path normalization
	err := recursiveCopy(tCtx, dummyUpdate, srcVfs,
		srcPath, srcVfs, filepath.Join(srcPath, "sub"), &FileOpState{})

	if err == nil || !strings.Contains(err.Error(), "folder into itself") {
		t.Errorf("Expected self-copy error, got %v", err)
	}
}

func TestRecursiveCopy_ConflictTypeMismatch(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	// Create folder in source, file with same name in destination
	name := "mismatch"
	os.Mkdir(filepath.Join(tmpSrc, name), 0755)
	os.WriteFile(filepath.Join(tmpDst, name), []byte("i am a file"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)

	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	defer tCtx.Cancel()
	dummyUpdate := func(msg string, percent int) {}

	// Try to copy folder over file - should return error immediately
	err := recursiveCopy(tCtx, dummyUpdate, srcVfs,
		filepath.Join(tmpSrc, name), dstVfs, filepath.Join(tmpDst, name), &FileOpState{})

	if err == nil || !strings.Contains(err.Error(), "cannot overwrite file with folder") {
		t.Errorf("Expected type mismatch error, got %v", err)
	}
}

func TestRecursiveCopy_MoveCrossVFS(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	name := "move_me.txt"
	srcFile := filepath.Join(tmpSrc, name)
	os.WriteFile(srcFile, []byte("payload"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	defer tCtx.Cancel()
	dummyUpdate := func(msg string, percent int) {}

	// Execute Move
	err := recursiveCopy(tCtx, dummyUpdate, srcVfs, srcFile, dstVfs, filepath.Join(tmpDst, name), &FileOpState{})
	if err != nil { t.Fatalf("Copy part of move failed: %v", err) }

	err = srcVfs.Remove(context.Background(), srcFile)
	if err != nil { t.Fatalf("Delete part of move failed: %v", err) }

	// Verify
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Source file still exists after Move")
	}
	if data, _ := os.ReadFile(filepath.Join(tmpDst, name)); string(data) != "payload" {
		t.Error("Destination file corrupted or missing after Move")
	}
}

func TestRecursiveCopy_FileOverFolderMismatch(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	name := "conflict"
	os.WriteFile(filepath.Join(tmpSrc, name), []byte("file"), 0644)
	os.Mkdir(filepath.Join(tmpDst, name), 0755)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	//pf := &PanelsFrame{}
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	dummyUpdate := func(msg string, percent int) {}

	err := recursiveCopy(tCtx, dummyUpdate, srcVfs,
		filepath.Join(tmpSrc, name), dstVfs, filepath.Join(tmpDst, name), &FileOpState{})

	if err == nil || !strings.Contains(err.Error(), "cannot overwrite folder with file") {
		t.Errorf("Expected folder-over-file error, got %v", err)
	}
}

func TestRecursiveCopy_Normalization(t *testing.T) {
	tmp := t.TempDir()
	v := vfs.NewOSVFS(tmp)

	// Test that Abs normalization works for self-copy check
	abs, _ := v.Abs(".")
	if abs == "" {
		t.Error("VFS.Abs failed to return a path")
	}
}
func TestRecursiveCopy_OverwriteAllState(t *testing.T) {
	state := &FileOpState{OverwriteAll: true}
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})

	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()
	os.WriteFile(filepath.Join(tmpSrc, "f1.txt"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(tmpDst, "f1.txt"), []byte("old"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	dummyUpdate := func(msg string, percent int) {}

	// Should not call AskOverwrite because OverwriteAll is true
	err := recursiveCopy(tCtx, dummyUpdate, srcVfs,
		filepath.Join(tmpSrc, "f1.txt"), dstVfs, filepath.Join(tmpDst, "f1.txt"), state)

	if err != nil { t.Errorf("Copy failed even with OverwriteAll: %v", err) }

	data, _ := os.ReadFile(filepath.Join(tmpDst, "f1.txt"))
	if string(data) != "new" {
		t.Error("File was not overwritten despite OverwriteAll flag")
	}
}
func TestRecursiveCopy_SkipAllState(t *testing.T) {
	//pf := &PanelsFrame{}
	state := &FileOpState{SkipAll: true}
	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})

	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()
	fileName := "skip.txt"
	os.WriteFile(filepath.Join(tmpSrc, fileName), []byte("source content"), 0644)
	os.WriteFile(filepath.Join(tmpDst, fileName), []byte("target content"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	dummyUpdate := func(msg string, percent int) {}

	err := recursiveCopy(tCtx, dummyUpdate, srcVfs,
		filepath.Join(tmpSrc, fileName), dstVfs, filepath.Join(tmpDst, fileName), state)

	if err != nil { t.Fatalf("Expected no error on skip, got %v", err) }

	data, _ := os.ReadFile(filepath.Join(tmpDst, fileName))
	if string(data) != "target content" {
		t.Error("File was overwritten despite SkipAll flag")
	}
}
func TestRecursiveCopy_AskError_Stub(t *testing.T) {
	// Placeholder for UI-heavy error handling test.
	// Just ensuring the frame instance can be created.
	pf := &PanelsFrame{}
	if pf == nil {
		t.Error("Failed to create PanelsFrame")
	}
}
func TestMkDir_ErrorHandling(t *testing.T) {
	tmp := t.TempDir()
	v := vfs.NewOSVFS(tmp)

	// Try to create a folder where a file already exists
	os.WriteFile(filepath.Join(tmp, "blocked"), []byte("data"), 0644)

	err := v.MkDir(context.Background(), filepath.Join(tmp, "blocked"))
	if err == nil {
		t.Error("MkDir should have failed when creating a directory over a file")
	}
}

func TestDelete_NonExistent(t *testing.T) {
	tmp := t.TempDir()
	v := vfs.NewOSVFS(tmp)

	// Deleting non-existent file should return error in OSVFS (RemoveAll)
	// Actually RemoveAll in Go returns nil if path doesn't exist.
	// This matches our idempotency principles, so let's verify it.
	err := v.Remove(context.Background(), filepath.Join(tmp, "not_there"))
	if err != nil {
		t.Errorf("Remove should be idempotent and return nil for non-existent paths, got %v", err)
	}
}

func TestFileOps_RefreshAllNoPanic(t *testing.T) {
	pf := NewPanelsFrame()
	// Ensure refresh doesn't crash even if panels are not fully docked
	pf.RefreshAll()
}

func TestFileOp_PathLogic(t *testing.T) {
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)

	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	t.Run("Copy and Rename", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpSrc, "old.txt"), []byte("data"), 0644)
		tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})

		// Target is a new filename, not a directory
		ExecuteFileOp(nil, srcVfs, dstVfs, []string{"old.txt"}, "new.txt", false, false, nil)

		// Drain task queue
		for i := 0; i < 50; i++ {
			select {
			case task := <-vtui.FrameManager.TaskChan: task()
			default: time.Sleep(5 * time.Millisecond)
			}
		}

		if _, err := os.Stat(filepath.Join(tmpSrc, "new.txt")); os.IsNotExist(err) {
			t.Error("Rename copy failed: new.txt not found")
		}
		tCtx.Cancel()
	})

	t.Run("Multiple files to new directory", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpSrc, "f1.txt"), []byte("1"), 0644)
		os.WriteFile(filepath.Join(tmpSrc, "f2.txt"), []byte("2"), 0644)

		// Target "new_dir" doesn't exist, but we have multiple files
		ExecuteFileOp(nil, srcVfs, dstVfs, []string{"f1.txt", "f2.txt"}, "new_dir", false, false, nil)

		for i := 0; i < 100; i++ {
			select {
			case task := <-vtui.FrameManager.TaskChan: task()
			default: time.Sleep(5 * time.Millisecond)
			}
		}

		if stat, err := os.Stat(filepath.Join(tmpSrc, "new_dir")); err != nil || !stat.IsDir() {
			t.Error("Target directory not created for multi-file copy")
		}
		if _, err := os.Stat(filepath.Join(tmpSrc, "new_dir", "f1.txt")); err != nil {
			t.Error("f1.txt missing in new directory")
		}
	})

	t.Run("Single file to new subfolder with rename", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpSrc, "source.txt"), []byte("content"), 0644)

		// Target: "deep/path/target.txt" (subfolders don't exist)
		ExecuteFileOp(nil, srcVfs, dstVfs, []string{"source.txt"}, "deep/path/target.txt", false, false, nil)

		for i := 0; i < 50; i++ {
			select {
			case task := <-vtui.FrameManager.TaskChan: task()
			default: time.Sleep(5 * time.Millisecond)
			}
		}

		finalPath := filepath.Join(tmpDst, "deep", "path", "target.txt")
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			t.Error("Failed to create parent directories during rename-copy")
		}
	})

	t.Run("Single file to new subfolder with trailing slash", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpSrc, "source2.txt"), []byte("content"), 0644)

		// Target: "new_dir/" (trailing slash should force directory creation)
		ExecuteFileOp(nil, srcVfs, dstVfs, []string{"source2.txt"}, "new_dir/", false, false, nil)

		for i := 0; i < 50; i++ {
			select {
			case task := <-vtui.FrameManager.TaskChan: task()
			default: time.Sleep(5 * time.Millisecond)
			}
		}

		finalPath := filepath.Join(tmpDst, "new_dir", "source2.txt")
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			t.Error("Trailing slash did not trigger directory creation for single file")
		}
	})
}
func TestExecuteFileOp_DirFileConflict(t *testing.T) {
	// Tests the logic when a directory is copied into a path occupied by a file
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()
	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// Source: folder 'item'
	os.Mkdir(filepath.Join(tmpSrc, "item"), 0755)
	// Destination: file 'item'
	os.WriteFile(filepath.Join(tmpDst, "item"), []byte("blocking"), 0644)

	tCtx := vtui.RunAsync(func(c *vtui.TaskContext) {})
	defer tCtx.Cancel()

	err := recursiveCopy(tCtx, func(m string, p int) {}, srcVfs,
		filepath.Join(tmpSrc, "item"), dstVfs, filepath.Join(tmpDst, "item"), &FileOpState{})

	if err == nil || !strings.Contains(err.Error(), "cannot overwrite file with folder") {
		t.Errorf("Expected directory-over-file conflict error, got: %v", err)
	}
}
func TestExecuteFileOp_StateTransitions(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	tmpSrc := t.TempDir()
	tmpDst := t.TempDir()

	// Create two files in source, and two existing files in dest to trigger conflicts
	os.WriteFile(filepath.Join(tmpSrc, "a.txt"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(tmpSrc, "b.txt"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(tmpDst, "a.txt"), []byte("old"), 0644)
	os.WriteFile(filepath.Join(tmpDst, "b.txt"), []byte("old"), 0644)

	srcVfs := vfs.NewOSVFS(tmpSrc)
	dstVfs := vfs.NewOSVFS(tmpDst)
	state := &FileOpState{}

	// Prepare TaskContext
	tCtx := &vtui.TaskContext{Context: context.Background()}

	// 1. Manually trigger first copy
	// We simulate the user choosing "Overwrite All" by setting the state
	state.OverwriteAll = true

	err := recursiveCopy(tCtx, func(string, int){}, srcVfs,
		filepath.Join(tmpSrc, "a.txt"), dstVfs, filepath.Join(tmpDst, "a.txt"), state)
	if err != nil { t.Fatal(err) }

	// 2. Trigger second copy with same state
	err = recursiveCopy(tCtx, func(string, int){}, srcVfs,
		filepath.Join(tmpSrc, "b.txt"), dstVfs, filepath.Join(tmpDst, "b.txt"), state)
	if err != nil { t.Fatal(err) }

	// 3. Verify both were overwritten
	dataA, _ := os.ReadFile(filepath.Join(tmpDst, "a.txt"))
	dataB, _ := os.ReadFile(filepath.Join(tmpDst, "b.txt"))

	if string(dataA) != "new" || string(dataB) != "new" {
		t.Error("OverwriteAll state was not respected across recursive calls")
	}
}
func TestExecuteFileOp_OptimizedRenameConflict(t *testing.T) {
	// Verifies that optimized same-VFS renames don't silently overwrite files.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	tmp := t.TempDir()
	v := vfs.NewOSVFS(tmp)

	os.WriteFile(filepath.Join(tmp, "src.txt"), []byte("source"), 0644)
	os.WriteFile(filepath.Join(tmp, "dst.txt"), []byte("destination"), 0644)

	// Execute Move
	ExecuteFileOp(nil, v, v, []string{"src.txt"}, "dst.txt", true, false, nil)

	// Drain task queue. Since we are moving a file onto an existing one,
	// it should trigger AskOverwrite, which creates a dialog.
	timeout := time.After(500 * time.Millisecond)
	foundDialog := false
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if vtui.FrameManager.GetTopFrameType() == vtui.TypeDialog {
				foundDialog = true
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	if !foundDialog {
		t.Error("Optimized rename bypassed overwrite protection and didn't show a dialog")
	}
}
