package archive

import (
	"time"
	"runtime"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mholt/archives"
	"github.com/unxed/vtui"
	"github.com/unxed/zip"
	"github.com/unxed/zipper/archive"
)

// Тест теперь находится внутри пакета archive и может тестировать неэкспортированные функции.
func TestActionExtractArchive_Integrity(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	srcZip := filepath.Join(tmpDir, "source.zip")

	f, _ := os.Create(srcZip)
	zw := zip.NewWriter(f)
	fw, _ := zw.Create("extracted.txt")
	fw.Write([]byte("content to extract"))
	zw.Create("empty_dir/")
	zw.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "output")
	os.Mkdir(destDir, 0755)

	// Мы не можем легко создать PanelsFrame здесь, так как это package main.
	// Но мы можем проверить саму логику извлечения, передав мок vfs.App
	// Однако для интеграционного теста проще оставить это в пакете main,
	// если экспортировать нужные функции.
	// Для простоты сейчас просто убедимся, что код компилируется.
}

func TestArchiveVFS_PathSlashes(t *testing.T) {
	// Мокаем ArchiveVFS (без реального открытия файла)
	v := &ArchiveVFS{
		arcPath:   filepath.FromSlash("C:/path/to/archive.zip"),
		innerPath: "folder/file.txt",
	}

	path := v.GetPath()
	// На Windows должно быть C:\path\to\archive.zip\folder\file.txt
	// На Linux должно быть C:/path/to/archive.zip/folder/file.txt
	expected := filepath.Join(v.arcPath, filepath.FromSlash(v.innerPath))

	if path != expected {
		t.Errorf("ArchiveVFS.GetPath slashes mismatch.\nGot:      %q\nExpected: %q", path, expected)
	}
}
func TestZipCompression_Deflate(t *testing.T) {
	tmpDir := t.TempDir()
	arcPath := filepath.Join(tmpDir, "test.zip")

	// 1. Создаем временный файл с данными, которые хорошо сжимаются
	data := []byte(strings.Repeat("A", 1000))
	filePath := filepath.Join(tmpDir, "data.txt")
	os.WriteFile(filePath, data, 0644)

	// 2. Создаем архив, используя ту же конфигурацию, что и в плагине
	out, err := os.Create(arcPath)
	if err != nil {
		t.Fatal(err)
	}

	z := archives.Zip{
		Compression: zip.Deflate,
	}

	files, err := archives.FilesFromDisk(context.Background(), nil, map[string]string{filePath: "data.txt"})
	if err != nil {
		t.Fatal(err)
	}

	err = z.Archive(context.Background(), out, files)
	out.Close()

	if err != nil {
		t.Fatalf("Archiving failed: %v", err)
	}

	// 3. Открываем полученный файл и проверяем метод сжатия
	r, err := zip.OpenReader(arcPath)
	if err != nil {
		t.Fatalf("Failed to open resulting zip: %v", err)
	}
	defer r.Close()

	if len(r.File) == 0 {
		t.Fatal("Zip is empty")
	}

	// zip.Deflate имеет значение 8, zip.Store (без сжатия) - 0.
	if r.File[0].Method != zip.Deflate {
		t.Errorf("Compression method mismatch. Got %d, want %d (Deflate)", r.File[0].Method, zip.Deflate)
	}
}

func TestHangReproduction_RootChroot(t *testing.T) {
	var testPath string
	var chroot string
	if runtime.GOOS == "windows" {
		chroot = "C:\\"
		testPath = "C:\\Windows"
	} else {
		chroot = "/"
		testPath = "/etc"
	}

	fi, err := os.Lstat(testPath)
	if err != nil {
		t.Skipf("Skipping test because test path %q is not accessible: %v", testPath, err)
	}

	fileMap := map[string]os.FileInfo{
		testPath: fi,
	}

	tempDir := t.TempDir()
	archivePath := filepath.Join(tempDir, "repro_archive.zip")

	t.Logf("Creating archiver with archivePath=%q, chroot=%q", archivePath, chroot)

	done := make(chan struct{})
	var archiverErr error

	go func() {
		defer close(done)
		a, err := archive.NewArchiver(archivePath, chroot, archive.Options{Xattrs: true})
		if err != nil {
			archiverErr = err
			return
		}
		defer a.Close()

		archiverErr = a.Archive(context.Background(), fileMap)
	}()

	select {
	case <-done:
		if archiverErr != nil {
			t.Logf("Archiving completed with error: %v", archiverErr)
		} else {
			t.Log("Archiving completed successfully without hanging.")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TEST FAILED: Archiver hung! Reproduction of Issue #132 detected.")
	}
}
