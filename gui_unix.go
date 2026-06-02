//go:build linux || darwin || openbsd || netbsd || dragonfly || freebsd || illumos || solaris

package main

import (
	"github.com/unxed/vtui"
)

func RunGui(backend string) error {
	return vtui.RunInGUIWindow(100, 30, backend, func() {
		SetupUI()
	})
}
