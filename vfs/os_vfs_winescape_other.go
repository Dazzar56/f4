//go:build !windows

package vfs

// winescapeReadDirNames is a no-op outside Windows: libwinescape's raw
// syscall path only exists to let a Windows/PE binary escape Win32 under
// Wine, which is meaningless when the binary is already native POSIX.
func winescapeReadDirNames(dirPath string) (names []string, ok bool) {
	return nil, false
}
