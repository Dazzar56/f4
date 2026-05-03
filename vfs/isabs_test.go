package vfs

import (
	"runtime"
	"testing"
)

func TestVFS_IsAbs_Implementations(t *testing.T) {
	t.Run("NullVFS (Unix semantics)", func(t *testing.T) {
		v := NewNullVFS(0)
		// Should be true on all platforms
		if !v.IsAbs("/home/user") {
			t.Error("NullVFS should treat '/home/user' as absolute")
		}
		if v.IsAbs("relative/path") {
			t.Error("NullVFS should treat 'relative/path' as relative")
		}
	})

	t.Run("OSVFS (Platform specific)", func(t *testing.T) {
		v := NewOSVFS(".")
		if runtime.GOOS == "windows" {
			if v.IsAbs("/unix/style") {
				t.Error("OSVFS on Windows should not treat '/unix/style' as absolute")
			}
			if !v.IsAbs("C:\\Windows") {
				t.Error("OSVFS on Windows should treat 'C:\\Windows' as absolute")
			}
		} else {
			if !v.IsAbs("/usr/bin") {
				t.Error("OSVFS on Unix should treat '/usr/bin' as absolute")
			}
		}
	})
}
