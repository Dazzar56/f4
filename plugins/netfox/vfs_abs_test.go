package netfox

import (
	"testing"
)

func TestSFTPVFS_Abs(t *testing.T) {
	v := &SFTPVFS{
		path: "/home/user/test",
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"file.txt", "/home/user/test/file.txt"},
		{"sub/dir", "/home/user/test/sub/dir"},
		{"/etc/passwd", "/etc/passwd"},
		{"/", "/"},
	}

	for _, tt := range tests {
		got, _ := v.Abs(tt.input)
		if got != tt.expected {
			t.Errorf("SFTPVFS.Abs(%q): expected %q, got %q", tt.input, tt.expected, got)
		}
	}
}

func TestFTPVFS_Abs(t *testing.T) {
	v := &FTPVFS{
		cwd: "/pub/files",
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"data.zip", "/pub/files/data.zip"},
		{"/root.txt", "/root.txt"},
		{"", "/pub/files"},
	}

	for _, tt := range tests {
		got, _ := v.Abs(tt.input)
		if got != tt.expected {
			t.Errorf("FTPVFS.Abs(%q): expected %q, got %q", tt.input, tt.expected, got)
		}
	}
}