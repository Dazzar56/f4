//go:build windows

package vfs

import "syscall"

func applyPlatformAttributes(path string, item VFSItem) error {
	if item.WinAttrs != 0 {
		ptr, err := syscall.UTF16PtrFromString(path)
		if err == nil {
			return syscall.SetFileAttributes(ptr, item.WinAttrs)
		}
	}
	return nil
}
