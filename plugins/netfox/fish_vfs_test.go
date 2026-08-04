package netfox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
)

// newLocalFishVFS runs the real helper in a local shell and wraps it in a
// FishVFS, which is the only way to check the mapping against output real
// tools produced rather than against captured samples.
func newLocalFishVFS(t *testing.T) *FishVFS {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell on Windows")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell available")
	}
	cmd := exec.Command(shell)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", shell, err)
	}
	v, err := NewFishVFSOnStream(context.Background(), nil, stdin, stdout, stdin, "local")
	if err != nil {
		cmd.Process.Kill()
		if strings.Contains(err.Error(), "base64") {
			t.Skipf("no base64 on this host: %v", err)
		}
		t.Fatalf("handshake: %v", err)
	}
	t.Cleanup(func() {
		v.Close()
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
	})
	return v
}

func TestFishVFSBrowsesLocalShell(t *testing.T) {
	v := newLocalFishVFS(t)
	ctx := context.Background()

	if !strings.HasPrefix(v.GetPath(), "/") {
		t.Errorf("GetPath = %q, want the remote working directory", v.GetPath())
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub dir", filepath.Join(dir, "link to dir")); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}

	if err := v.SetPath(dir); err != nil {
		t.Fatalf("SetPath(%q): %v", dir, err)
	}
	if v.GetPath() != filepath.Clean(dir) {
		t.Errorf("GetPath = %q, want %q", v.GetPath(), dir)
	}
	if err := v.SetPath(filepath.Join(dir, "a file.txt")); err == nil {
		t.Error("SetPath accepted a regular file")
	}

	byName := map[string]vfs.VFSItem{}
	if err := v.ReadDir(ctx, ".", func(chunk []vfs.VFSItem) {
		for _, item := range chunk {
			byName[item.Name] = item
		}
	}); err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(byName) != 4 {
		t.Fatalf("got %d entries: %v", len(byName), byName)
	}

	file, ok := byName["a file.txt"]
	if !ok {
		t.Fatal("a file with a space in its name got lost")
	}
	if file.Size != 5 || file.IsDir || file.IsSymlink {
		t.Errorf("file mapped wrong: %+v", file)
	}
	if file.UnixMode&0777 != 0644 {
		t.Errorf("UnixMode = %o, want 644 in the low bits", file.UnixMode)
	}
	if time.Since(file.MTime) > time.Hour {
		t.Errorf("MTime = %v, which is nowhere near now", file.MTime)
	}

	if hidden, ok := byName[".hidden"]; !ok {
		t.Error("hidden file missing")
	} else if !hidden.IsHidden {
		t.Error("a dot file was not marked hidden")
	}

	if sub, ok := byName["sub dir"]; !ok || !sub.IsDir {
		t.Errorf("subdirectory mapped wrong: %+v", sub)
	}

	link, ok := byName["link to dir"]
	if !ok {
		t.Fatal("symlink missing")
	}
	if !link.IsSymlink {
		t.Error("symlink not marked as one")
	}
	if !link.IsDir {
		t.Error("a symlink to a directory must be enterable")
	}
}

func TestFishVFSStatAndOpen(t *testing.T) {
	v := newLocalFishVFS(t)
	ctx := context.Background()

	dir := t.TempDir()
	body := strings.Repeat("0123456789", 5000)
	p := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(p, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	item, err := v.Stat(ctx, p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if item.Name != "payload.bin" || item.Size != int64(len(body)) {
		t.Errorf("Stat mapped wrong: %+v", item)
	}
	if !item.IsExecutable {
		t.Error("an executable file was not marked as one")
	}
	if _, err := v.Stat(ctx, filepath.Join(dir, "no such file")); err == nil {
		t.Error("Stat of a missing file succeeded")
	}

	f, err := v.Open(ctx, p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if f.Size() != int64(len(body)) {
		t.Errorf("Size = %d, want %d", f.Size(), len(body))
	}
	buf := make([]byte, 20)
	if n, err := f.ReadAt(ctx, buf, 12345); err != nil || n != len(buf) {
		t.Fatalf("ReadAt = %d, %v", n, err)
	}
	if string(buf) != body[12345:12365] {
		t.Errorf("ReadAt returned %q", buf)
	}

	if !v.GetCapabilities().HasRandomAccess {
		t.Error("HasRandomAccess must be set, the viewer depends on it")
	}
}

func TestFishVFSMutationsRefuseForNow(t *testing.T) {
	v := newLocalFishVFS(t)
	ctx := context.Background()
	dir := t.TempDir()

	if err := v.MkDir(ctx, filepath.Join(dir, "new")); err != ErrFishReadOnly {
		t.Errorf("MkDir = %v, want ErrFishReadOnly", err)
	}
	if err := v.Remove(ctx, dir); err != ErrFishReadOnly {
		t.Errorf("Remove = %v, want ErrFishReadOnly", err)
	}
	if err := v.Rename(ctx, dir, dir+".x"); err != ErrFishReadOnly {
		t.Errorf("Rename = %v, want ErrFishReadOnly", err)
	}
	if _, err := v.Create(ctx, filepath.Join(dir, "new")); err != ErrFishReadOnly {
		t.Errorf("Create = %v, want ErrFishReadOnly", err)
	}
	if err := v.SetAttributes(ctx, dir, vfs.VFSItem{}); err != ErrFishReadOnly {
		t.Errorf("SetAttributes = %v, want ErrFishReadOnly", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("a refused mutation still touched the disk: %v", err)
	}
}
func TestFishVFSCloneHasItsOwnDirectory(t *testing.T) {
	v := newLocalFishVFS(t)
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := v.SetPath(dirA); err != nil {
		t.Fatalf("SetPath: %v", err)
	}

	clone, ok := v.Clone().(*FishVFS)
	if !ok {
		t.Fatal("Clone did not return a FishVFS")
	}
	if clone == v {
		t.Fatal("Clone returned the same instance, so two panels would share one current directory")
	}
	if clone.GetPath() != v.GetPath() {
		t.Errorf("clone starts at %q, want %q", clone.GetPath(), v.GetPath())
	}
	if err := clone.SetPath(dirB); err != nil {
		t.Fatalf("SetPath on the clone: %v", err)
	}
	if v.GetPath() != filepath.Clean(dirA) {
		t.Errorf("moving the clone moved the original to %q", v.GetPath())
	}
	if clone.GetPath() != filepath.Clean(dirB) {
		t.Errorf("the clone did not move: %q", clone.GetPath())
	}
}

func TestFishVFSSessionOutlivesItsClones(t *testing.T) {
	v := newLocalFishVFS(t)
	ctx := context.Background()
	dir := t.TempDir()
	clone := v.Clone().(*FishVFS)

	if err := clone.Close(); err != nil {
		t.Fatalf("closing the clone: %v", err)
	}
	if _, err := v.Stat(ctx, dir); err != nil {
		t.Fatalf("closing a clone tore the shared session down: %v", err)
	}
	// A double close must not drop the reference count twice.
	if err := clone.Close(); err != nil {
		t.Errorf("closing the clone again: %v", err)
	}
	if _, err := v.Stat(ctx, dir); err != nil {
		t.Fatalf("a second close of the clone tore the session down: %v", err)
	}

	if err := v.Close(); err != nil {
		t.Fatalf("closing the last view: %v", err)
	}
	if _, err := v.Stat(ctx, dir); err == nil {
		t.Error("the session survived its last user")
	}
}

func TestFishVFSClonesShareOneSessionSafely(t *testing.T) {
	v := newLocalFishVFS(t)
	dir := t.TempDir()
	for _, name := range []string{"one", "two", "three"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	clone := v.Clone().(*FishVFS)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 16; i++ {
		for _, target := range []*FishVFS{v, clone} {
			wg.Add(1)
			go func(x *FishVFS) {
				defer wg.Done()
				count := 0
				err := x.ReadDir(context.Background(), dir, func(chunk []vfs.VFSItem) {
					count += len(chunk)
				})
				if err != nil {
					errs <- err
					return
				}
				if count != 3 {
					errs <- fmt.Errorf("listing returned %d entries, want 3", count)
				}
			}(target)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent use of one session: %v", err)
	}
}
func TestFishProtocolIsRegistered(t *testing.T) {
	found := false
	for _, p := range GetProtocols() {
		if p == "fish+" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fish+ missing from the protocol list: %v", GetProtocols())
	}
	ph := &fishProtocolHandler{}
	if ph.Prefix() != "fish+" {
		t.Errorf("Prefix = %q", ph.Prefix())
	}
	if ph.DefaultPort() != "22" {
		t.Errorf("DefaultPort = %q, want 22", ph.DefaultPort())
	}
	ui, apply := ph.BuildExtraUI(&NetFoxConfig{}, 0, 0, 10, 10)
	if ui != nil {
		t.Error("the fish+ handler needs no extra UI yet")
	}
	apply()
}

func TestFishTypeMatches(t *testing.T) {
	for _, good := range []string{"fish+", "fish"} {
		if !fishTypeMatches(good) {
			t.Errorf("type %q not recognized as FISH+", good)
		}
	}
	// An empty type belongs to SFTP, which claims it as its default; taking
	// it here would hijack every site saved before FISH+ existed.
	for _, bad := range []string{"", "sftp", "ftp", "fishy"} {
		if fishTypeMatches(bad) {
			t.Errorf("type %q wrongly claimed by FISH+", bad)
		}
	}
}

func TestSSHTimeoutDefaults(t *testing.T) {
	if got := sshTimeout(0); got != 15*time.Second {
		t.Errorf("sshTimeout(0) = %v, want 15s", got)
	}
	if got := sshTimeout(-3); got != 15*time.Second {
		t.Errorf("sshTimeout(-3) = %v, want 15s", got)
	}
	if got := sshTimeout(7); got != 7*time.Second {
		t.Errorf("sshTimeout(7) = %v, want 7s", got)
	}
}

func TestDialSSHFailsOnAClosedPort(t *testing.T) {
	client, err := DialSSH("127.0.0.1", "1", "nobody", "", 2)
	if err == nil {
		client.Close()
		t.Fatal("dialing a closed port succeeded")
	}
}
