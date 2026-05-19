//go:build !windows

package vfs

func applyPlatformAttributes(path string, item VFSItem) error {
	return nil
}