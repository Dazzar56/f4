//go:build !windows

package vfs

import "os"

func resolveWindowsJunction(path string) (string, error) {
	return "", os.ErrInvalid
}
