package archive

import (
	"path/filepath"
	"testing"
)

func TestArchiveVFS_Abs(t *testing.T) {
	arcPath := filepath.FromSlash("/tmp/test.zip")
	v := &ArchiveVFS{
		arcPath:   arcPath,
		innerPath: "folder",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Relative path inside archive",
			input:    "file.txt",
			expected: "/tmp/test.zip/folder/file.txt",
		},
		{
			name:     "Absolute path (full path with archive)",
			input:    "/tmp/test.zip/other",
			expected: "/tmp/test.zip/other",
		},
		{
			name:     "Root-style path inside archive",
			input:    "/manual/root",
			expected: "/manual/root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Normalize expected to slash because ArchiveVFS.Join uses ToSlash
			exp := filepath.ToSlash(filepath.Clean(tt.expected))
			got, _ := v.Abs(tt.input)
			if got != exp {
				t.Errorf("ArchiveVFS.Abs(%q): expected %q, got %q", tt.input, exp, got)
			}
		})
	}
}
