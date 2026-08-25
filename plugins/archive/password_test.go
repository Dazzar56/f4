package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/f4/vfs"
	zipperarchive "github.com/unxed/zipper/archive"
)

func TestArchiveVFS_PromptsForEncryptedZip(t *testing.T) {
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "secret.zip")
	filePath := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(filePath, []byte("secret data"), 0600); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	archiver, err := zipperarchive.NewArchiver(archivePath, tmp, zipperarchive.Options{Password: "correct"})
	if err != nil {
		t.Fatal(err)
	}
	if err := archiver.Archive(context.Background(), map[string]os.FileInfo{filePath: info}); err != nil {
		t.Fatal(err)
	}
	if err := archiver.Close(); err != nil {
		t.Fatal(err)
	}

	prompts := 0
	previousPrompt := archivePasswordPrompt
	archivePasswordPrompt = func(context.Context, string) (string, error) {
		prompts++
		return "correct", nil
	}
	defer func() { archivePasswordPrompt = previousPrompt }()

	v, err := NewArchiveVFS(vfs.NewOSVFS(tmp), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	file, err := v.Open(context.Background(), v.Join(archivePath, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	data, err := io.ReadAll(ctxReader{r: file, ctx: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret data" {
		t.Fatalf("got %q, want %q", data, "secret data")
	}
	if prompts != 1 {
		t.Fatalf("password prompt count = %d, want 1", prompts)
	}
}
