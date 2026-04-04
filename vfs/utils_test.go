package vfs

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsTerminalRunnable(t *testing.T) {
	tmpDir := t.TempDir()
	v := NewOSVFS(tmpDir)
	ctx := context.Background()

	// 1. Regular text file
	txtFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(txtFile, []byte("hello"), 0644)
	if IsTerminalRunnable(ctx, v, txtFile) {
		t.Error("Text file should not be terminal-runnable")
	}

	// 2. Shell script by extension
	shFile := filepath.Join(tmpDir, "test.sh")
	os.WriteFile(shFile, []byte("echo hi"), 0644)
	if !IsTerminalRunnable(ctx, v, shFile) {
		t.Error(".sh file should be terminal-runnable")
	}

	// 3. File with shebang
	sbFile := filepath.Join(tmpDir, "myscript")
	os.WriteFile(sbFile, []byte("#!/usr/bin/env python\nprint('hi')"), 0644)
	if !IsTerminalRunnable(ctx, v, sbFile) {
		t.Error("File with shebang should be terminal-runnable")
	}

	// 4. Directory should not be runnable
	subDir := filepath.Join(tmpDir, "folder")
	os.Mkdir(subDir, 0755)
	if IsTerminalRunnable(ctx, v, subDir) {
		t.Error("Directory should not be terminal-runnable")
	}

	// 5. Unix Executable Bit
	if runtime.GOOS != "windows" {
		binFile := filepath.Join(tmpDir, "mybin")
		os.WriteFile(binFile, []byte{0x7f, 'E', 'L', 'F'}, 0755)
		if !IsTerminalRunnable(ctx, v, binFile) {
			t.Error("Executable bit should make file runnable on Unix")
		}
	}
}

func TestIsTerminalRunnable_ShebangVariations(t *testing.T) {
	tmpDir := t.TempDir()
	v := NewOSVFS(tmpDir)
	ctx := context.Background()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"Standard Bash", "#!/bin/bash\nexit 0", true},
		{"Env Python", "#!/usr/bin/env python3\nprint(1)", true},
		{"Short shebang", "#!", true}, // Minimum possible
		{"No shebang", "echo hi", false},
		{"Space before shebang", " #!/bin/sh", false}, // Invalid shebang
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, "script_"+tt.name)
			os.WriteFile(path, []byte(tt.content), 0644)
			if got := IsTerminalRunnable(ctx, v, path); got != tt.want {
				t.Errorf("IsTerminalRunnable() for content %q = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}
