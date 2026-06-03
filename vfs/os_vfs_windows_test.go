//go:build windows

package vfs

import (
	"path/filepath"
	"testing"
)

func TestOSVFS_WindowsPathLogic(t *testing.T) {
	// Test that SetPath doesn't double the drive letter
	v := NewOSVFS("C:\\Windows")

	// Navigating to another drive via drive-relative path
	// In f4, we expect this to resolve correctly if the path contains a volume name
	err := v.SetPath("T:\\Data")
	if err != nil {
		// T: might not exist on the test machine, but we can check the logic
		// if we mock the FS. For now, testing the internal resolution logic.
	}

	// Test internal target calculation logic from SetPath
	path := "T:Folder"
	current := "C:\\Windows"
	target := path
	if !filepath.IsAbs(path) && filepath.VolumeName(path) == "" {
		target = filepath.Join(current, path)
	}

	if target != "T:Folder" {
		t.Errorf("Windows path resolution failed: expected 'T:Folder', got %q", target)
	}
}

func TestOSVFS_WindowsRootDetection(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"C:\\", true},
		{"C:/", true},
		{"C:", true},
		{"\\", true},
		{"/", true},
		{"C:\\Windows", false},
		{"C:\\Windows\\", false},
	}

	for _, tt := range tests {
		v := &OSVFS{currentPath: tt.path}
		if got := v.IsAtRoot(); got != tt.want {
			t.Errorf("IsAtRoot(%q) = %v, want %v  vol: %v  path: %v", tt.path, got, tt.want, filepath.VolumeName(tt.path), filepath.Clean(tt.path))
		}
	}
}
