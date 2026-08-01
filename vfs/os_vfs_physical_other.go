//go:build !unix && !windows

package vfs

import "os"

// fillPhysicalSize stub for platforms without a portable way to
// obtain per-file allocation info. Leaves item.PhysicalSize at 0;
// consumers should hide their "physical size" UI when the accumulated
// total is 0.
func fillPhysicalSize(item *VFSItem, _ os.FileInfo, _ string) {}
