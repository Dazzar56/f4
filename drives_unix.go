//go:build !windows

package main

import "github.com/unxed/f4/vfs"

func getPlatformDrives() []DriveEntry {
	return []DriveEntry{
		{Name: "&/. Root", Factory: func() vfs.VFS { return vfs.NewOSVFS("/") }},
	}
}