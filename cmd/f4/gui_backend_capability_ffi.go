//go:build linux || darwin || windows

package main

import "github.com/go-webgpu/goffi/ffi"

func ffiAvailableForGUI() bool {
	return ffi.Available()
}
