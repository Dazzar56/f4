//go:build linux || darwin || openbsd || netbsd || dragonfly

package main

import (
	"os"
	"fmt"
	"github.com/unxed/vtui"
)

func RunGui(backend string) error {
	// Запускаем f4 в графическом окне 100x30 символов
	err := vtui.RunInGUIWindow(100, 30, backend, func() {
		SetupUI()
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "GUI Startup Error: %v\n", err)
		os.Exit(1)
	}

	return nil
}
