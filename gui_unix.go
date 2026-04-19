//go:build !windows

package main

import "github.com/unxed/vtui"

func RunGui() {
	// Запускаем f4 в X11 окне 100x30 символов
	vtui.RunInX11Window(100, 30, SetupUI)
}