//go:build linux

package vfs

import (
	"os"
	"syscall"
	"time"
)

func fillPlatformTimes(item *VFSItem, info os.FileInfo) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		item.ATime = time.Unix(int64(stat.Atim.Sec), int64(stat.Atim.Nsec))
		item.CTime = time.Unix(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec))
		item.Uid = int(stat.Uid)
		item.Gid = int(stat.Gid)
	}
}
