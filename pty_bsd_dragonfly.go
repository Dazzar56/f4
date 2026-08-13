//go:build dragonfly

package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ptySlaveName asks the master which pts unit number it is paired with.
// DragonFly BSD uses unix98 pts devices under /dev/pts/N.
// TIOCGPTN is _IOR('t', 15, int) = 0x4004740f in sys/sys/ttycom.h.
func ptySlaveName(masterFd int) (string, error) {
	const tiocgptn = 0x4004740f
	n, err := unix.IoctlGetInt(masterFd, tiocgptn)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/dev/pts/%d", n), nil
}
