package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
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

type mockAppForProgress struct {
	t           *testing.T
	activeVfs   vfs.VFS
	passiveVfs  vfs.VFS
	names       []string
	progressPct []int
	progressMsg []string
	done        chan struct{}
	mu          sync.Mutex
}

func (m *mockAppForProgress) GetActivePanelVFS() vfs.VFS      { return m.activeVfs }
func (m *mockAppForProgress) GetPassivePanelVFS() vfs.VFS     { return m.passiveVfs }
func (m *mockAppForProgress) GetSelectedNames() []string      { return m.names }
func (m *mockAppForProgress) GetSelectedName() string         { return m.names[0] }
func (m *mockAppForProgress) RefreshAll()                     {}
func (m *mockAppForProgress) SetPendingSelection(name string) {}
func (m *mockAppForProgress) RunProgressTask(title, startMsg string, forked bool, worker func(ctx context.Context, update func(msg string, percent int)) error, onComplete func(err error)) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	update := func(msg string, percent int) {
		m.mu.Lock()
		m.progressPct = append(m.progressPct, percent)
		m.progressMsg = append(m.progressMsg, msg)
		m.mu.Unlock()
	}

	err := worker(ctx, update)
	onComplete(err)
	close(m.done)
}
func (m *mockAppForProgress) Message(title, msg string, buttons []string) int { return 0 }
func (m *mockAppForProgress) InputBox(title, prompt, defaultText string, callback func(string)) {
	callback(defaultText)
}
func (m *mockAppForProgress) Menu(title string, items []string, callback func(int)) {}

func TestActionExtractArchive_ProgressUpdates(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	srcZip := filepath.Join(tmpDir, "test_progress.zip")

	f, _ := os.Create(srcZip)
	zw := zip.NewWriter(f)
	fw, _ := zw.Create("file.txt")
	fw.Write([]byte("some data"))
	zw.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "output")
	os.Mkdir(destDir, 0755)

	activeVfs := vfs.NewOSVFS(tmpDir)
	passiveVfs := vfs.NewOSVFS(destDir)

	app := &mockAppForProgress{
		t:          t,
		activeVfs:  activeVfs,
		passiveVfs: passiveVfs,
		names:      []string{"test_progress.zip"},
		done:       make(chan struct{}),
	}

	actionExtractArchive(app)
	<-app.done

	app.mu.Lock()
	defer app.mu.Unlock()

	if len(app.progressPct) == 0 {
		t.Error("Extraction progress percentage was never updated")
	}

	hasSpeedInfo := false
	for _, msg := range app.progressMsg {
		if strings.Contains(msg, "/s |") && strings.Contains(msg, "files") && strings.Contains(msg, "Extracting:") {
			hasSpeedInfo = true
			break
		}
	}
	if !hasSpeedInfo {
		t.Errorf("Expected extraction status message to contain real progress (speed and files), got: %v", app.progressMsg)
	}
}

func TestActionAddArchive_ProgressUpdates(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("data"), 0644)

	activeVfs := vfs.NewOSVFS(tmpDir)

	app := &mockAppForProgress{
		t:          t,
		activeVfs:  activeVfs,
		passiveVfs: activeVfs,
		names:      []string{"file1.txt"},
		done:       make(chan struct{}),
	}

	actionAddArchive(app)
	<-app.done

	app.mu.Lock()
	defer app.mu.Unlock()

	if len(app.progressPct) == 0 {
		t.Error("Archiving progress percentage was never updated")
	}

	hasSpeedInfo := false
	for _, msg := range app.progressMsg {
		if strings.Contains(msg, "/s |") && strings.Contains(msg, "files") && strings.Contains(msg, "Archiving:") {
			hasSpeedInfo = true
			break
		}
	}
	if !hasSpeedInfo {
		t.Errorf("Expected archiving status message to contain real progress (speed and files), got: %v", app.progressMsg)
	}
}
