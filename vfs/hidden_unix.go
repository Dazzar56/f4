//go:build !windows

package vfs

import (
	"os"
	"strings"
)

func isHidden(path string, name string, info os.FileInfo) bool {
	return strings.HasPrefix(name, ".")
}
