package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
)

type FoundFile struct {
	Path string
	Item vfs.VFSItem
}

// ExecuteFindFile initiates a background search and displays a progress dialog.
func ExecuteFindFile(pf *PanelsFrame, v vfs.VFS, startDir, mask, text string) {
	dlg := vtui.NewCenteredDialog(60, 9, " Searching... ")
	dlg.AttentionSuppressed = true

	lblMask := vtui.NewLabel(0, 0, "Mask: "+mask, nil)
	lblDir := vtui.NewLabel(0, 0, "Scanning: ...", nil)
	lblFound := vtui.NewLabel(0, 0, "Found: 0", nil)

	btnCancel := vtui.NewButton(0, 0, "Cancel")

	dlg.AddItem(lblMask)
	dlg.AddItem(lblDir)
	dlg.AddItem(lblFound)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 60-4, 9-4)
	vbox.Add(lblMask, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(lblDir, vtui.Margins{Top: 1}, vtui.AlignLeft)

	hbox := vtui.NewHBoxLayout(0, 0, 60-4, 1)
	hbox.Add(lblFound, vtui.Margins{}, vtui.AlignLeft)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignRight)
	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	var taskCtx *vtui.TaskContext
	btnCancel.OnClick = func() {
		if taskCtx != nil {
			taskCtx.Cancel()
		}
		dlg.Close()
	}

	// Since we are inside an action handler (UI thread), we can push directly
	vtui.FrameManager.AddScreenHeadless(dlg)

	taskCtx = vtui.RunAsync(func(ctx *vtui.TaskContext) {
		// Parse masks (e.g. "*.go, *.txt")
		masks := strings.Split(mask, ",")
		for i := range masks {
			masks[i] = strings.TrimSpace(masks[i])
			// Far compatibility: *.* translates to * in filepath.Match logic
			masks[i] = strings.ReplaceAll(masks[i], "*.*", "*")
		}
		if len(masks) == 0 || mask == "" {
			masks = []string{"*"}
		}

		searchTextLower := strings.ToLower(text)
		var found []FoundFile
		var count int

		var walk func(dir string) error
		walk = func(dir string) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			ctx.RunOnUI(func() {
				display := runewidth.Truncate(dir, 56, "...")
				lblDir.SetText("Scanning: " + display)
				vtui.FrameManager.Redraw()
			})

			return v.ReadDir(ctx.Context, dir, func(chunk []vfs.VFSItem) {
				for _, item := range chunk {
					if ctx.Err() != nil { return }
					if item.Name == ".." { continue }

					itemPath := v.Join(dir, item.Name)

					if item.IsDir {
						_ = walk(itemPath) // Ignore permissions/read errors to continue walking
					} else {
						// 1. Check Mask
						matched := false
						for _, m := range masks {
							if m == "" { continue }
							match, _ := filepath.Match(m, item.Name)
							if match {
								matched = true
								break
							}
						}
						if !matched { continue }

						// 2. Check Text Content
						if text != "" {
							if !fileContainsText(ctx.Context, v, itemPath, searchTextLower) {
								continue
							}
						}

						// 3. Register Hit
						count++
						found = append(found, FoundFile{Path: itemPath, Item: item})
						ctx.RunOnUI(func() {
							lblFound.SetText(fmt.Sprintf("Found: %d", count))
							vtui.FrameManager.Redraw()
						})
					}
				}
			})
		}

		err := walk(startDir)

		ctx.RunOnUI(func() {
			dlg.Close()
			if err != nil && err != context.Canceled {
				vtui.ShowMessage(" Error ", fmt.Sprintf("Search failed:\n%v", err), []string{"&Ok"})
			} else if len(found) == 0 {
				vtui.ShowMessage(" Find File ", "File not found.", []string{"&Ok"})
			} else {
				ShowSearchResults(pf, v, found)
			}
		})
	})
}

// fileContainsText scans a file for a substring using chunked reads.
// It handles overlaps to ensure words crossing chunk boundaries are found.
func fileContainsText(ctx context.Context, v vfs.VFS, path string, textLower string) bool {
	f, err := v.Open(ctx, path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 128*1024) // 128KB chunks
	overlap := len(textLower) - 1
	if overlap < 0 {
		overlap = 0
	}

	var tail []byte

	for {
		if ctx.Err() != nil {
			return false
		}

		n, err := f.Read(ctx, buf)
		if n > 0 {
			data := buf[:n]

			// Prepend tail from previous chunk
			if len(tail) > 0 {
				data = append(tail, data...)
			}

			if strings.Contains(strings.ToLower(string(data)), textLower) {
				return true
			}

			// Save the tail for the next overlap
			if len(data) > overlap {
				// Append to nil to force a new allocation, avoiding memory pinning
				tail = append([]byte(nil), data[len(data)-overlap:]...)
			} else {
				tail = append([]byte(nil), data...)
			}
		}
		if err != nil {
			break // EOF or error
		}
	}
	return false
}

type foundFileRow struct {
	ff FoundFile
	v  vfs.VFS
}

func (r foundFileRow) GetCellText(col int) string {
	switch col {
	case 0:
		return r.ff.Item.Name
	case 1:
		if r.ff.Item.IsDir {
			return "<DIR>"
		}
		return fmt.Sprintf("%d", r.ff.Item.Size)
	case 2:
		return r.v.Dir(r.ff.Path)
	}
	return ""
}

type SearchResultsWindow struct {
	vtui.Window
	table *vtui.Table
	found []FoundFile
	vfs   vfs.VFS
	pf    *PanelsFrame
}

func (srw *SearchResultsWindow) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown {
		return srw.Window.ProcessKey(e)
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_F3:
		return srw.HandleCommand(CmView, nil)
	case vtinput.VK_F4:
		return srw.HandleCommand(CmEdit, nil)
	}

	return srw.Window.ProcessKey(e)
}

func (srw *SearchResultsWindow) HandleCommand(cmd int, args any) bool {
	idx := srw.table.SelectPos
	if idx >= 0 && idx < len(srw.found) {
		ff := srw.found[idx]
		switch cmd {
		case CmView:
			actionOpenViewer(srw.pf, srw.vfs, ff.Path)
			return true
		case CmEdit:
			actionOpenEditor(srw.pf, srw.vfs, ff.Path)
			return true
		}
	}
	return srw.Window.HandleCommand(cmd, args)
}

func (srw *SearchResultsWindow) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			"", "", "View", "Edit", "", "", "", "", "", "Quit", "", "",
		},
	}
}

func ShowSearchResults(pf *PanelsFrame, v vfs.VFS, found []FoundFile) {
	dlgW, dlgH := 76, 20
	baseDlg := vtui.NewCenteredDialog(dlgW, dlgH, " Search Results ")

	srw := &SearchResultsWindow{
		Window: *baseDlg,
		found:  found,
		vfs:    v,
		pf:     pf,
	}

	cols := []vtui.TableColumn{
		{Title: "Name", Width: 20},
		{Title: "Size", Width: 10, Alignment: vtui.AlignRight},
		{Title: "Path", Width: 38},
	}
	srw.table = vtui.NewTable(0, 0, 72, 12, cols)
	srw.table.ShowScrollBar = true

	rows := make([]vtui.TableRow, len(found))
	for i, ff := range found {
		rows[i] = foundFileRow{ff, v}
	}
	srw.table.SetRows(rows)

	btnGo := vtui.NewButton(0, 0, "&Go to")
	btnView := vtui.NewButton(0, 0, "&View")
	btnEdit := vtui.NewButton(0, 0, "&Edit")
	btnClose := vtui.NewButton(0, 0, "&Close")

	btnGo.IsDefault = true

	doGoTo := func() {
		idx := srw.table.SelectPos
		if idx >= 0 && idx < len(found) {
			ff := found[idx]
			srw.Close()
			if fsp := pf.getActivePanel(); fsp != nil {
				fsp.vfs.SetPath(v.Dir(ff.Path))
				fsp.pendingSelection = v.Base(ff.Path)
				fsp.ReadDirectory()
				pf.showPanels = true
			}
		}
	}

	srw.table.OnAction = func(idx int) { doGoTo() }
	btnGo.OnClick = doGoTo
	btnClose.OnClick = func() { srw.Close() }
	btnView.OnClick = func() { srw.HandleCommand(CmView, nil) }
	btnEdit.OnClick = func() { srw.HandleCommand(CmEdit, nil) }

	vbox := vtui.NewVBoxLayout(srw.X1+2, srw.Y1+2, dlgW-4, dlgH-4)
	vbox.Add(srw.table, vtui.Margins{Bottom: 1}, vtui.AlignFill)

	hbox := vtui.NewHBoxLayout(0, 0, dlgW-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnGo, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnView, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnEdit, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnClose, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{}, vtui.AlignFill)
	vbox.Apply()

	srw.AddItem(srw.table)
	srw.AddItem(btnGo)
	srw.AddItem(btnView)
	srw.AddItem(btnEdit)
	srw.AddItem(btnClose)

	vtui.FrameManager.Push(srw)
}
