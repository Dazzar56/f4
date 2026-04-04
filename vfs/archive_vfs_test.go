package vfs

import (
	"archive/zip"
	"context"
	"os"
	"time"
	"fmt"
	"path/filepath"
	"testing"
)

func createTestZip(t *testing.T, path string) {
	f, err := os.Create(path)
	if err != nil { t.Fatal(err) }
	defer f.Close()

	zw := zip.NewWriter(f)
	// Добавляем файл
	fw, _ := zw.Create("file.txt")
	fw.Write([]byte("archive content"))
	// Добавляем папку
	zw.Create("subdir/")
	// Добавляем файл в папке
	fw2, _ := zw.Create("subdir/inner.txt")
	fw2.Write([]byte("inner data"))

	zw.Close()
}

func TestArchiveVFS_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, err := NewArchiveVFS(osVfs, zipPath)
	if err != nil {
		t.Fatalf("Failed to open ArchiveVFS: %v", err)
	}

	ctx := context.Background()

	t.Run("ReadDir Root", func(t *testing.T) {
		var items []VFSItem
		err := arcVfs.ReadDir(ctx, "", func(chunk []VFSItem) {
			items = append(items, chunk...)
		})
		if err != nil { t.Fatal(err) }

		foundFile := false
		foundDir := false
		for _, itm := range items {
			if itm.Name == "file.txt" && !itm.IsDir { foundFile = true }
			if itm.Name == "subdir" && itm.IsDir { foundDir = true }
		}
		if !foundFile || !foundDir {
			t.Errorf("ReadDir failed. Items: %v", items)
		}
	})

	t.Run("Stat inner file", func(t *testing.T) {
		path := filepath.Join(zipPath, "subdir/inner.txt")
		info, err := arcVfs.Stat(ctx, path)
		if err != nil { t.Fatal(err) }
		if info.Name != "inner.txt" || info.Size != 10 {
			t.Errorf("Stat failed: %+v", info)
		}
	})

	t.Run("Open (Temp Extraction)", func(t *testing.T) {
		path := filepath.Join(zipPath, "file.txt")
		rc, err := arcVfs.Open(ctx, path)
		if err != nil { t.Fatal(err) }

		// Проверяем, что это временный файл
		trc, ok := rc.(*tempReadAtCloser)
		if !ok { t.Fatal("Open did not return a tempReadAtCloser") }

		tempPath := trc.tempPath
		if _, err := os.Stat(tempPath); err != nil {
			t.Error("Temp file does not exist on disk")
		}

		// Читаем данные
		buf := make([]byte, 100)
		n, _ := rc.Read(ctx, buf)
		if string(buf[:n]) != "archive content" {
			t.Errorf("Data mismatch: %q", string(buf[:n]))
		}

		// Проверяем удаление при закрытии
		rc.Close()
		if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
			t.Error("Temp file was not deleted after Close")
		}
	})
}

func TestArchiveVFS_CreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "write.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)
	ctx := context.Background()

	// 1. Создаем новый файл внутри ZIP
	newFileName := "folder/new.txt"
	fullPath := arcVfs.Join(zipPath, newFileName)

	wc, err := arcVfs.Create(ctx, fullPath)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	content := "hello zip"
	wc.Write([]byte(content))
	err = wc.Close()
	if err != nil {
		t.Fatalf("Close (save to zip) failed: %v", err)
	}

	// 2. Проверяем, что файл появился в архиве
	info, err := arcVfs.Stat(ctx, fullPath)
	if err != nil {
		t.Fatalf("Stat of new file failed: %v", err)
	}
	if info.Size != int64(len(content)) {
		t.Errorf("Wrong size: got %d, want %d", info.Size, len(content))
	}

	// 3. Проверяем содержимое
	rc, _ := arcVfs.Open(ctx, fullPath)
	buf := make([]byte, 20)
	n, _ := rc.Read(ctx, buf)
	rc.Close()
	if string(buf[:n]) != content {
		t.Errorf("Content mismatch: got %q", string(buf[:n]))
	}
}
func TestArchiveVFS_MkDir_Recursive(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "mkdir.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)
	ctx := context.Background()

	// 1. Создаем глубокую папку
	newPath := filepath.Join(zipPath, "a/b/c/")
	err := arcVfs.MkDir(ctx, newPath)
	if err != nil {
		t.Fatalf("MkDir failed: %v", err)
	}

	// 2. Проверяем Stat
	info, err := arcVfs.Stat(ctx, newPath)
	if err != nil || !info.IsDir {
		t.Errorf("Stat after MkDir failed: %v", err)
	}

	// 3. Проверяем ReadDir промежуточной папки
	var items []VFSItem
	arcVfs.ReadDir(ctx, filepath.Join(zipPath, "a/b"), func(chunk []VFSItem) {
		items = append(items, chunk...)
	})

	found := false
	for _, itm := range items {
		if itm.Name == "c" && itm.IsDir { found = true }
	}
	if !found {
		t.Error("Intermediate directory 'c' not found in 'a/b'")
	}
}

func TestArchiveVFS_Stress_ConcurrentReadWrite(t *testing.T) {
	// Проверяем, что одновременное чтение не падает при модификации
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "stress.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)
	ctx := context.Background()

	done := make(chan bool)

	// Читатель
	go func() {
		for i := 0; i < 50; i++ {
			var items []VFSItem
			_ = arcVfs.ReadDir(ctx, "", func(chunk []VFSItem) {
				items = append(items, chunk...)
			})
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Писатель
	go func() {
		for i := 0; i < 10; i++ {
			_ = arcVfs.MkDir(ctx, filepath.Join(zipPath, fmt.Sprintf("dir%d", i)))
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ { <-done }
}
func TestArchiveVFS_DeepNavigation(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "deep.zip")

	// Создаем архив с глубокой вложенностью
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	zw.Create("a/b/c/d/file.txt")
	zw.Close()
	f.Close()

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)

	// Проверяем последовательный вход
	arcVfs.SetPath(zipPath + "/a")
	arcVfs.SetPath(arcVfs.GetPath() + "/b")
	arcVfs.SetPath(arcVfs.GetPath() + "/c/d")

	expected := filepath.ToSlash(filepath.Join(zipPath, "a/b/c/d"))
	if arcVfs.GetPath() != expected {
		t.Errorf("Deep path mismatch. Got %q, want %q", arcVfs.GetPath(), expected)
	}

	// Проверяем IsAtRoot после глубокого заплыва
	if arcVfs.IsAtRoot() {
		t.Error("Should not be at root")
	}

	// Возврат в корень
	arcVfs.SetPath(zipPath)
	if !arcVfs.IsAtRoot() {
		t.Error("Should be at root now")
	}
}

func TestArchiveVFS_Stat_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "missing.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)
	ctx := context.Background()

	// 1. Пытаемся получить Stat для несуществующего файла
	_, err := arcVfs.Stat(ctx, arcVfs.Join(zipPath, "phantom.txt"))
	if err == nil {
		t.Error("Stat should return error for non-existent file in archive")
	}
}


func TestArchiveVFS_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "concurrent.zip")
	createTestZip(t, zipPath)

	osVfs := NewOSVFS(tmpDir)
	arcVfs, _ := NewArchiveVFS(osVfs, zipPath)
	ctx := context.Background()

	// Запускаем 10 параллельных чтений
	const workers = 10
	done := make(chan bool)

	for i := 0; i < workers; i++ {
		go func(id int) {
			var items []VFSItem
			err := arcVfs.ReadDir(ctx, "", func(chunk []VFSItem) {
				items = append(items, chunk...)
			})
			if err != nil {
				t.Errorf("Worker %d failed: %v", id, err)
			}
			done <- true
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-done
	}
}

func TestArchiveVFS_InvalidArchive(t *testing.T) {
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "not_an_archive.txt")
	os.WriteFile(badFile, []byte("I am just a text file"), 0644)

	osVfs := NewOSVFS(tmpDir)

	// Теперь мы тестируем как "клиент": спрашиваем у реестра,
	// есть ли провайдер для этого файла.
	// Регистрируем провайдер вручную, так как в тестах пакет main не импортируется.
	RegisterProvider(&ArchiveProvider{})

	provider := FindProvider(context.Background(), osVfs, badFile)
	if provider != nil {
		t.Errorf("FindProvider found a provider for non-archive file %s", badFile)
	}
}
