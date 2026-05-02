//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDir_Isolation(t *testing.T) {
	dir := sessionDir()
	expectedSuffix := fmt.Sprintf("f4-sessions-%d", os.Getuid())

	if filepath.Base(dir) != expectedSuffix {
		t.Errorf("sessionDir() = %q; want suffix %q", dir, expectedSuffix)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("sessionDir was not created: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("sessionDir permissions = %v; want 0700", info.Mode().Perm())
	}
}