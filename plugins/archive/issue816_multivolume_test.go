package archive

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unxed/f4/vfs"
)

func TestArchiveVFS_MultiVolumeRar(t *testing.T) {
	sourceDir := archivesRarFixtureDir(t)
	tmp := t.TempDir()
	for _, name := range []string{"test.part01.rar", "test.part02.rar"} {
		data, err := os.ReadFile(filepath.Join(sourceDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	v, err := NewArchiveVFS(vfs.NewOSVFS(tmp), "test.part01.rar")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	var currentItems []vfs.VFSItem
	if err := v.ReadDir(context.Background(), v.GetPath(), func(items []vfs.VFSItem) {
		currentItems = append(currentItems, items...)
	}); err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(currentItems) != 1 || currentItems[0].Name != "test.txt" {
		t.Fatalf("entries = %#v", currentItems)
	}
	file, err := v.Open(context.Background(), v.Join(v.GetPath(), "test.txt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, readErr := io.ReadAll(ctxReader{r: file, ctx: context.Background()})
	closeErr := file.Close()
	if closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	hash := sha1.Sum(data)
	if got := hex.EncodeToString(hash[:]); got != "4da7f88f69b44a3fdb705667019a65f4c6e058a3" {
		t.Fatalf("RAR entry SHA-1 = %s, want the complete multi-volume payload", got)
	}

	dstDir := filepath.Join(tmp, "out")
	if err := os.Mkdir(dstDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := v.CopyBulk(context.Background(), []string{"test.txt"}, vfs.NewOSVFS(dstDir), dstDir, &dummyReporter{}); err != nil {
		t.Fatalf("CopyBulk: %v", err)
	}
	copyData, err := os.ReadFile(filepath.Join(dstDir, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if copyHash := sha1.Sum(copyData); hex.EncodeToString(copyHash[:]) != "4da7f88f69b44a3fdb705667019a65f4c6e058a3" {
		t.Fatalf("CopyBulk payload differs from the complete multi-volume entry")
	}
}

func archivesRarFixtureDir(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMODCACHE")
	output, err := cmd.Output()
	if err != nil {
		t.Skipf("cannot locate Go module cache: %v", err)
	}
	root := strings.TrimSpace(string(output))
	matches, err := filepath.Glob(filepath.Join(root, "github.com", "unxed", "archives@*", "testdata"))
	if err != nil {
		t.Skipf("locating archives RAR fixture: %v", err)
	}
	for _, dir := range matches {
		if _, err := os.Stat(filepath.Join(dir, "test.part01.rar")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "test.part02.rar")); err == nil {
				return dir
			}
		}
	}
	t.Skip("archives RAR fixture is unavailable")
	return ""
}
