//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"github.com/unxed/f4/vfs"
)

func getPlatformDrives() []DriveEntry {
	var drives []DriveEntry
	bitmask, err := windows.GetLogicalDrives()
	if err != nil {
		return drives
	}
	for i := 0; i < 26; i++ {
		if bitmask&(1<<uint(i)) != 0 {
			letter := string(rune('A' + i))
			path := letter + ":\\"
			drives = append(drives, DriveEntry{
				Name: "&" + letter + ": Local",
				Factory: func() vfs.VFS { return vfs.NewOSVFS(path) },
			})
		}
	}
	return drives
}