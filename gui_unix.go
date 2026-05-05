//go:build linux || darwin || openbsd || netbsd || dragonfly

package main

import "github.com/unxed/vtui"

func RunGui(backend string) error {
	// Запускаем f4 в графическом окне 100x30 символов
	return vtui.RunInGUIWindow(100, 30, backend, func() {
		SetupUI()
	})
}
