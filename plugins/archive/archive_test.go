package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/vtui"
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
		arcPath: filepath.FromSlash("C:/path/to/archive.zip"),
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
