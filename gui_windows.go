//go:build windows

package main

import (
	"strings"

	"github.com/unxed/vtui"
	"golang.org/x/sys/windows"
)

func RunGui(backend string) error {
	if backend == "qt" || strings.HasPrefix(backend, "ext:") {
		_ = windows.FreeConsole()
		return RunExternalUIWithMapping(backend)
	}
	return vtui.RunInGUIWindow(100, 30, backend, AppConfig.GuiFont, float64(AppConfig.GuiFontSize), func() {
		_ = windows.FreeConsole()
		SetupUI()
	})
}
