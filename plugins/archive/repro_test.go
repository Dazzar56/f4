package archive

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
	"github.com/unxed/zipper/archive"
)

type mockOverwriteApp struct {
	t              *testing.T
	v              vfs.VFS
	names          []string
	messageCalled  bool
	progressCalled bool
	done           chan struct{}
}

func (m *mockOverwriteApp) GetActivePanelVFS() vfs.VFS  { return m.v }
func (m *mockOverwriteApp) GetPassivePanelVFS() vfs.VFS { return m.v }
func (m *mockOverwriteApp) GetSelectedNames() []string  { return m.names }
func (m *mockOverwriteApp) GetSelectedName() string     { return m.names[0] }
func (m *mockOverwriteApp) RefreshAll()                 {}
func (m *mockOverwriteApp) RunProgressTask(title, startMsg string, forked bool, worker func(ctx context.Context, update func(msg string, percent int)) error, onComplete func(err error)) {
	m.progressCalled = true
	close(m.done)
}
func (m *mockOverwriteApp) Message(title, msg string, buttons []string) int {
	m.messageCalled = true
	m.t.Logf("mockApp.Message called: %q - %q", title, msg)
	return 0
}
func (m *mockOverwriteApp) InputBox(title, prompt, defaultText string, callback func(string)) {
	m.t.Logf("mockApp.InputBox called with defaultText: %q", defaultText)
	callback(defaultText)
}
func (m *mockOverwriteApp) Menu(title string, items []string, callback func(int)) {}

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

func TestActionAddArchive_OverwriteWarning(t *testing.T) {
	tmpDir := t.TempDir()
	v := vfs.NewOSVFS(tmpDir)

	dummyFile := v.Join(tmpDir, "file_to_archive.txt")
	os.WriteFile(dummyFile, []byte("some content"), 0644)

	archiveName := v.Base(tmpDir) + ".zip"
	existingArchive := v.Join(tmpDir, archiveName)
	os.WriteFile(existingArchive, []byte("existing zip content"), 0644)

	app := &mockOverwriteApp{
		t:     t,
		v:     v,
		names: []string{"file_to_archive.txt"},
		done:  make(chan struct{}),
	}

	t.Log("Calling actionAddArchive...")
	actionAddArchive(app)

	<-app.done

	if !app.messageCalled {
		t.Fatal("TEST FAILED: actionAddArchive silently overwrote the archive! Overwrite warning dialog was NOT shown.")
	}
	t.Log("SUCCESS: Overwrite warning dialog was shown before archiving.")
}

func TestIssue137_ArchiveOpenIsLazyAndContextAware(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "large.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("large.bin")
	chunk := []byte(strings.Repeat("A", 1024*1024))
	for i := 0; i < 5; i++ {
		w.Write(chunk)
	}
	zw.Close()
	f.Close()

	vArc, err := NewArchiveVFS(&vfs.OSVFS{}, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer vArc.Close()

	// Test 1: Open should be fast and not block
	start := time.Now()
	rc, errOpen := vArc.Open(context.Background(), vArc.Join(zipPath, "large.bin"))
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("BUG REPRODUCED: Open took too long, likely synchronously extracting!")
	}
	defer rc.Close()

	// Test 2: ReadAt respects cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	buf := make([]byte, 100)
	_, errRead := rc.ReadAt(ctx, buf, 0)
	if errRead != context.Canceled {
		t.Fatalf("Expected context.Canceled from ReadAt, got: %v", errRead)
	}

	// Test 3: Read respects cancellation
	_, errRead2 := rc.Read(ctx, buf)
	if errRead2 != context.Canceled {
		t.Fatalf("Expected context.Canceled from Read, got: %v", errRead2)
	}
}
