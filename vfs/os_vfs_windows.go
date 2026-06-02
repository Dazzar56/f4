//go:build windows

package vfs

import (
	"os"
	"syscall"
	"time"
)

func fillPlatformTimes(item *VFSItem, info os.FileInfo) {
	if stat, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		item.ATime = time.Unix(0, stat.LastAccessTime.Nanoseconds())
		item.CTime = time.Unix(0, stat.CreationTime.Nanoseconds())
		item.WinAttrs = stat.FileAttributes
	}
}
