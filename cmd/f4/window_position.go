package main

import "github.com/unxed/vtui"

// saveGuiWindowPosition captures the native GUI position for the explicit
// Shift+F9 settings save. It deliberately does not run during ordinary
// session saves: the user asked for the position to change only when settings
// are saved from the application.
func saveGuiWindowPosition() bool {
	x, y, ok := vtui.GetWindowPosition()
	if !ok {
		return false
	}
	AppConfig.GuiPosX = x
	AppConfig.GuiPosY = y
	AppConfig.GuiPositionSaved = true
	return true
}

// restoreGuiWindowPosition applies a position saved by Shift+F9. GUI hosts
// call this from their setup callback while the native window is still being
// initialized, so the first visible frame appears at the saved location.
func restoreGuiWindowPosition() {
	if AppConfig.GuiPositionSaved {
		vtui.SetWindowPosition(AppConfig.GuiPosX, AppConfig.GuiPosY)
	}
}
