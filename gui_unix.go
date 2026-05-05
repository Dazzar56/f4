//go:build linux || darwin || openbsd || netbsd || dragonfly

package main

import "github.com/unxed/vtui"

func RunGui() error {
	// Запускаем f4 в X11 окне 100x30 символов
	return vtui.RunInX11Window(100, 30, SetupUI)
}
