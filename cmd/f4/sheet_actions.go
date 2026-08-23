package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/sheet"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// sheetActionVisible keeps the command out of contexts where a new workspace
// would make no sense.
func sheetActionVisible() bool {
	if vtui.FrameManager == nil {
		return false
	}
	_, ok := vtui.FrameManager.GetTopFrame().(*PanelsFrame)
	return ok
}

// findSheetWorkspace switches to an existing spreadsheet workspace, if any.
func findSheetWorkspace() bool {
	if vtui.FrameManager == nil {
		return false
	}
	for index, screen := range vtui.FrameManager.Screens {
		if screen == nil {
			continue
		}
		for _, frame := range screen.Frames {
			if _, ok := frame.(*SheetFrame); ok {
				vtui.FrameManager.SwitchScreen(index)
				return true
			}
		}
	}
	return false
}

// actionSpreadsheet opens the spreadsheet, loading the file under the cursor
// when it is one this editor understands.
func actionSpreadsheet() bool {
	if vtui.FrameManager == nil {
		return false
	}
	path := selectedSpreadsheetPath()
	if path == "" && findSheetWorkspace() {
		return true
	}
	frame := NewSheetFrame()
	if path != "" {
		sheetOpen(frame, path)
	}
	vtui.FrameManager.AddScreenHeadless(frame)
	return true
}

// selectedSpreadsheetPath returns the panel selection when it looks like a
// spreadsheet: the native SQLite format, a workbook or a CSV file.
func selectedSpreadsheetPath() string {
	pf, ok := vtui.FrameManager.GetTopFrame().(*PanelsFrame)
	if !ok || pf == nil {
		return ""
	}
	name := pf.GetSelectedName()
	if name == "" || name == ".." {
		return ""
	}
	fs, ok := pf.GetActivePanelVFS().(*vfs.OSVFS)
	if !ok || fs == nil {
		return ""
	}
	path, err := fs.Abs(fs.Join(fs.GetPath(), name))
	if err != nil {
		return ""
	}
	path = filepath.Clean(path)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".csv":
		return path
	case ".f4s", ".db", ".sqlite", ".sqlite3":
		if sheet.IsSheetFile(context.Background(), path) {
			return path
		}
	}
	return ""
}

func init() {
	RegisterAction(Action{
		Name:        "App.Spreadsheet",
		Area:        "Shell",
		Label:       "Spreadsheet",
		LabelKey:    "Action.App.Spreadsheet",
		Description: "Open the spreadsheet workspace",
		DescKey:     "Action.App.Spreadsheet.Desc",
		DefaultKeys: []string{"ShiftF11"},
		MenuPath:    "Commands",
		Visible:     sheetActionVisible,
		Handler:     actionSpreadsheet,
	})
}
