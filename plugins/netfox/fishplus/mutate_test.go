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

	// The rename above moved the file out of "three" and into its parent,
	// so the parent is the one that is not empty now.
	if err := c.RemoveDir(ctx, filepath.Dir(moved)); err == nil {
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

func TestMutationsRequireSafePaths(t *testing.T) {
	c := newLocalShellClient(t)
	ctx := context.Background()
	dir := t.TempDir()

	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	// filepath.Join would clean the ".." away, which is exactly the kind of
	// path the helper must not have to trust, so these are built by hand.
	unsafe := []string{"", "/", "relative/path", dir + "/../escape", "/..", dir + "/.."}
	for _, p := range unsafe {
		if err := c.MkDir(ctx, p); err == nil {
			t.Errorf("mkdir accepted %q", p)
		}
		if err := c.Remove(ctx, p); err == nil {
			t.Errorf("rm accepted %q", p)
		}
		if err := c.RemoveDir(ctx, p); err == nil {
			t.Errorf("rmdir accepted %q", p)
		}
		if err := c.RemoveAll(ctx, p); err == nil {
			t.Errorf("rmtree accepted %q", p)
		}
		if err := c.Chmod(ctx, p, 0644); err == nil {
			t.Errorf("chmod accepted %q", p)
		}
		if err := c.Rename(ctx, victim, p); err == nil {
			t.Errorf("mv accepted %q as a destination", p)
		}
		if err := c.Rename(ctx, p, filepath.Join(dir, "landing")); err == nil {
			t.Errorf("mv accepted %q as a source", p)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a refused mutation moved the file anyway: %v", err)
	}

	// A name that merely starts with dots is not a ".." component and must
	// stay usable, or the guard has grown too wide.
	dotty := filepath.Join(dir, "..hidden")
	if err := os.WriteFile(dotty, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Chmod(ctx, dotty, 0600); err != nil {
		t.Errorf("a name starting with dots was refused: %v", err)
	}
	if err := c.Remove(ctx, dotty); err != nil {
		t.Errorf("removing a name starting with dots: %v", err)
	}

	resp, err := c.Session().ExecPath(ctx, "chmod", dir, "99z")
	if err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if resp.OK() {
		t.Error("chmod accepted a mode that is not octal")
	}
}
