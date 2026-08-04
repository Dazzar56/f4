package fishplus

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMutationsAgainstLocalShell(t *testing.T) {
	c := newLocalShellClient(t)
	ctx := context.Background()
	root := t.TempDir()

	nested := filepath.Join(root, "one two", "three")
	if err := c.MkDir(ctx, nested); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Fatalf("mkdir did not create %q: %v", nested, err)
	}
	// Creating it again must not fail, the panel may race with itself.
	if err := c.MkDir(ctx, nested); err != nil {
		t.Errorf("mkdir of an existing directory: %v", err)
	}

	file := filepath.Join(nested, "a file.txt")
	if err := os.WriteFile(file, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "one two", "moved file.txt")
	if err := c.Rename(ctx, file, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("the source of a rename survived")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("rename did not produce %q: %v", moved, err)
	}

	if err := c.Chmod(ctx, moved, 0100600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	info, err := os.Stat(moved)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
	if err := c.Chmod(ctx, moved, 0644); err != nil {
		t.Fatalf("chmod back: %v", err)
	}

	if err := c.RemoveDir(ctx, nested); err == nil {
		t.Error("rmdir removed a directory that is not empty")
	}
	if err := c.Remove(ctx, moved); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := os.Stat(moved); err == nil {
		t.Error("rm left the file behind")
	}
	// A missing file is not an error: rm -f is what the helper runs.
	if err := c.Remove(ctx, moved); err != nil {
		t.Errorf("rm of a missing file: %v", err)
	}
	if err := c.RemoveDir(ctx, nested); err != nil {
		t.Fatalf("rmdir of an empty directory: %v", err)
	}

	deep := filepath.Join(root, "tree", "a", "b")
	if err := c.MkDir(ctx, deep); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "leaf"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveAll(ctx, filepath.Join(root, "tree")); err != nil {
		t.Fatalf("rmtree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tree")); err == nil {
		t.Error("rmtree left the tree behind")
	}
}

func TestMutationsRefuseTheRootAndBadModes(t *testing.T) {
	c := newLocalShellClient(t)
	ctx := context.Background()

	for _, p := range []string{"/", ""} {
		if err := c.RemoveAll(ctx, p); err == nil {
			t.Errorf("rmtree accepted %q", p)
		}
		if err := c.Remove(ctx, p); err == nil {
			t.Errorf("rm accepted %q", p)
		}
		if err := c.RemoveDir(ctx, p); err == nil {
			t.Errorf("rmdir accepted %q", p)
		}
	}

	dir := t.TempDir()
	resp, err := c.Session().ExecPath(ctx, "chmod", dir, "99z")
	if err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if resp.OK() {
		t.Error("chmod accepted a mode that is not octal")
	}
}