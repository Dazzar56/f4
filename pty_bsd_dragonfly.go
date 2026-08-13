//go:build dragonfly

package main

import (
	"bytes"
	"syscall"
	"unsafe"
)

// ptySlaveName is carried over unchanged from the time DragonFly shared its
// implementation with FreeBSD. It is almost certainly broken in the same way
// the FreeBSD one was: 0x40807448 decodes to _IOR('t', 72, char[128]), and
// DragonFly's sys/sys/ttycom.h defines no command 72 in the 't' group.
//
// It is left alone on purpose rather than "fixed" by analogy. DragonFly has
// no TIOCGPTN, and its libc names the slave through fdevname_r() on the
// master, which is a different mechanism from the one FreeBSD uses; writing
// that blind, with no DragonFly machine to run it on, is what produced issue
// #444 in the first place. Anyone with access to DragonFly should replace
// this with the fdevname path and verify it there.
func ptySlaveName(masterFd int) (string, error) {
	const tiocptygname = 0x40807448

	ptyName := make([]byte, 128)
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(masterFd), tiocptygname, uintptr(unsafe.Pointer(&ptyName[0]))); e != 0 {
		return "", e
	}

	nameLen := bytes.IndexByte(ptyName, 0)
	if nameLen == -1 {
		nameLen = len(ptyName)
	}
	return string(ptyName[:nameLen]), nil
}
