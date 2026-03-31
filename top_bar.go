package main

import (
	"github.com/unxed/vtui"
)

// TopBar is a generic top status bar used by Editor and Viewer.
type TopBar struct {
	vtui.Bar
	GetValue func() string
}

func NewTopBar(cb func() string) *TopBar {
	return &TopBar{GetValue: cb}
}

func (tb *TopBar) Show(scr *vtui.ScreenBuf) {
	tb.Bar.Show(scr)
	if !tb.IsVisible() || tb.GetValue == nil {
		return
	}
	attr := vtui.Palette[ColViewerStatus]
	tb.DrawBackground(scr, attr)
	scr.Write(tb.X1, tb.Y1, vtui.StringToCharInfo(tb.GetValue(), attr))
}