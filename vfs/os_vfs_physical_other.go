//go:build !unix && !windows

package vfs

import "os"

// fillPhysicalSizeCheap and fillPhysicalSize are stubs on platforms
// without a portable way to obtain per-file allocation info. Both
// leave item.PhysicalSize at 0; consumers should hide their "physical
// size" UI when the accumulated total is 0.
func fillPhysicalSizeCheap(_ *VFSItem, _ os.FileInfo)      {}
func fillPhysicalSize(_ *VFSItem, _ os.FileInfo, _ string) {}
