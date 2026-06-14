package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mholt/archives"
	"github.com/unxed/vtui"
	"github.com/unxed/zip"
)

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
}

func TestZipCompression_Deflate(t *testing.T) {
	tmpDir := t.TempDir()
	arcPath := filepath.Join(tmpDir, "test.zip")

	data := []byte(strings.Repeat("A", 1000))
	filePath := filepath.Join(tmpDir, "data.txt")
	os.WriteFile(filePath, data, 0644)

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

	r, err := zip.OpenReader(arcPath)
	if err != nil {
		t.Fatalf("Failed to open resulting zip: %v", err)
	}
	defer r.Close()

	if len(r.File) == 0 {
		t.Fatal("Zip is empty")
	}

	if r.File[0].Method != zip.Deflate {
		t.Errorf("Compression method mismatch. Got %d, want %d (Deflate)", r.File[0].Method, zip.Deflate)
	}
}
