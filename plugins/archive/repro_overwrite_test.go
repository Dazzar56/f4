package archive

import (
	"context"
	"os"
	"testing"

	"github.com/unxed/f4/vfs"
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
	return 0 // Simulate user selecting the first option (e.g., "Yes" or "Overwrite")
}
func (m *mockOverwriteApp) InputBox(title, prompt, defaultText string, callback func(string)) {
	m.t.Logf("mockApp.InputBox called with defaultText: %q", defaultText)
	callback(defaultText)
}
func (m *mockOverwriteApp) Menu(title string, items []string, callback func(int)) {}

func TestActionAddArchive_OverwriteWarning(t *testing.T) {
	tmpDir := t.TempDir()
	v := vfs.NewOSVFS(tmpDir)

	// Create a dummy file to archive
	dummyFile := v.Join(tmpDir, "file_to_archive.txt")
	os.WriteFile(dummyFile, []byte("some content"), 0644)

	// Pre-create the target archive so it already exists
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

	// Wait for the background goroutine to reach RunProgressTask
	<-app.done

	if !app.messageCalled {
		t.Fatal("TEST FAILED: actionAddArchive silently overwrote the archive! Overwrite warning dialog was NOT shown.")
	}
	t.Log("SUCCESS: Overwrite warning dialog was shown before archiving.")
}