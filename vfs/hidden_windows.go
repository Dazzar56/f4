//go:build windows

package vfs

import (
	"os"
	"strings"
	"syscall"
)

func isHidden(path string, name string, info os.FileInfo) bool {
	if info != nil {
		if stat, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			if stat.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0 {
				return true
			}
		}
	}
	// Fallback to dot-prefix for cross-platform consistency
	return strings.HasPrefix(name, ".")
}
