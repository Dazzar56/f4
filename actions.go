package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

var (
	LastFindFileMask = "*"
	LastFindFileText = ""
	LastLeftPath     = ""
	LastRightPath    = ""
	LastLeftCursor   = ""
	LastRightCursor  = ""
	LastActivePanel  = 1
)
func actionOpenEditor(pf *PanelsFrame, v vfs.VFS, path string) {
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		var f vfs.ReadAtCloser
		var pt *piecetable.PieceTable
		var buf *AsyncBuffer

		if v != nil {
			// Safety check: prevent opening directories for editing
			if stat, err := v.Stat(ctx.Context, path); err == nil && stat.IsDir {
				ctx.RunOnUI(func() {
					vtui.ShowMessage(" Error ", "Cannot edit a directory.", []string{"&Ok"})
				})
				return
			}

			var err error
			f, err = v.Open(ctx.Context, path)
			if err != nil {
				vtui.DebugLog("actionOpenEditor: Open failed (assuming new file): %v", err)
				f = nil
			}
		}

		if f != nil {
			buf = NewAsyncBuffer(ctx.Context, f)
			pt = piecetable.NewWithBuffer(buf)
		} else {
			pt = piecetable.New(nil)
		}

		ctx.RunOnUI(func() {
			if ctx.Err() != nil {
				if buf != nil { buf.Close() }
				if f != nil { f.Close() }
				return
			}

			editor := NewEditorView(pt, v, path)
			if GlobalFileState != nil && path != "" {
				if state := GlobalFileState.GetState(path); state != nil {
					editor.WordWrap = state.EditorWrap
					editor.targetLine = state.EditorLine
					editor.targetPos = state.EditorPos
					editor.targetTopRow = state.EditorTopRow
					editor.targetLeft = state.EditorLeft
				}
			}
			editor.file = f
			editor.asyncBuf = buf
			editor.ResizeConsole(pf.lastW, pf.lastH)
			editor.StartIndexing()

			vtui.FrameManager.AddScreen(editor)
		})
	})
}

func actionOpenViewer(pf *PanelsFrame, v vfs.VFS, path string) {
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		if v != nil {
			// Safety check: prevent opening directories for viewing
			if stat, err := v.Stat(ctx.Context, path); err == nil && stat.IsDir {
				ctx.RunOnUI(func() {
					vtui.ShowMessage(" Error ", "Cannot view a directory.", []string{"&Ok"})
				})
				return
			}
		}

		viewer, err := NewViewerView(ctx.Context, v, path)
		ctx.RunOnUI(func() {
			if ctx.Err() != nil {
				if err == nil {
					viewer.Close()
				}
				return
			}
			if err != nil {
				vtui.DebugLog("PANELS: Failed to open viewer for %s: %v", path, err)
				vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to open file:\n%v", err), []string{"&Ok"})
				return
			}
			if GlobalFileState != nil && path != "" {
				if state := GlobalFileState.GetState(path); state != nil {
					viewer.TopOffset = state.ViewerOffset
					if viewer.TopOffset > viewer.backend.Size() {
						viewer.TopOffset = viewer.backend.Size() - 1
					}
					if viewer.TopOffset < 0 { viewer.TopOffset = 0 }
					viewer.WrapMode = state.ViewerWrap
					viewer.HexMode = state.ViewerHex
				}
			}
			viewer.ResizeConsole(pf.lastW, pf.lastH)
			vtui.FrameManager.AddScreen(viewer)
		})
	})
}

func actionViewerSearch(vv *ViewerView) {
	vtui.InputBox(Msg("Viewer.SearchTitle"), "Search for:", "", func(pattern string) {
		if pattern == "" { return }
		title := " Searching... "
		msg := fmt.Sprintf("Looking for: %s", pattern)
		
		vtui.FrameManager.PostTask(func() {
			dlg := vtui.NewCenteredDialog(50, 8, title)
			lbl := vtui.NewLabel(0, 0, msg, nil)
			dlg.AddItem(lbl)
			btnCancel := vtui.NewButton(0, 0, "&Cancel")
			dlg.AddItem(btnCancel)

			vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, 8-4)
			vbox.Add(lbl, vtui.Margins{}, vtui.AlignCenter)
			vbox.Add(btnCancel, vtui.Margins{Top: 1}, vtui.AlignCenter)
			vbox.Apply()

			vtui.FrameManager.AddScreenHeadless(dlg)
			
			_ = vtui.RunAsync(func(ctx *vtui.TaskContext) {
				btnCancel.OnClick = func() { ctx.Cancel(); dlg.Close() }
				foundOffset := int64(-1)
				currOff := vv.TopOffset + 1
				fileSize := vv.backend.Size()
				patternLower := strings.ToLower(pattern)
				
				for currOff < fileSize {
					if ctx.Err() != nil { return }
					percent := int((currOff * 100) / fileSize)
					ctx.RunOnUI(func() { dlg.SetProgress(percent) })
					
					data, err := vv.backend.ReadAt(currOff, 256*1024)
					if err == piecetable.ErrLoading {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					if err != nil || len(data) == 0 { break }
					
					idx := strings.Index(strings.ToLower(string(data)), patternLower)
					if idx != -1 {
						foundOffset = currOff + int64(idx)
						break
					}
					currOff += int64(len(data)) - int64(len(patternLower))
					if currOff < 0 { currOff = 0 }
				}
				
				ctx.RunOnUI(func() {
					dlg.Close()
					if foundOffset != -1 {
						vv.TopOffset = vv.backend.FindLineStart(foundOffset)
						vtui.FrameManager.Redraw()
					} else if ctx.Err() == nil {
						vtui.ShowMessage(" Search ", "Pattern not found.", []string{"&Ok"})
					}
				})
			})
		})
	})
}

func actionExecute(pf *PanelsFrame, v vfs.VFS, dir, name, path string) {
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		runnable := vfs.IsTerminalRunnable(ctx.Context, v, path)
		ctx.RunOnUI(func() {
			if runnable {
				activePty := pf.getActivePTY()
				if activePty != nil {
					cmd := name
					// Wrap command in Title sequences to signal f4 about managed execution state.
					// We use && so f4:done is only sent if the command succeeded.
					var cmdToWire string
				if runtime.GOOS == "windows" {
					// Windows CMD: Use `title` command because standard `echo` doesn't evaluate ANSI escapes.
					// We use %q to ensure name with spaces is handled correctly as a single command.
					cmdToWire = fmt.Sprintf("cd /d %q & title f4:busy & %q && title f4:done\r", dir, cmd)
				} else {
					// On Unix, use single quotes for paths to prevent Bash history expansion (the '!' problem).
					// We also disable history expansion explicitly with 'set +H'.
					sqDir := strings.ReplaceAll(dir, "'", "'\\''")
					sqCmd := strings.ReplaceAll(cmd, "'", "'\\''")
					cmdToWire = fmt.Sprintf(" set +H; cd '%s' && { printf \"\\033]2;f4:busy\\007\"; ./'%s' && printf \"\\033]2;f4:done\\007\"; }\r", sqDir, sqCmd)
				}
				vtui.DebugLog("ACTIONS: Sending to PTY: %q", cmdToWire)

				cleanCmd := "./" + cmd
				if runtime.GOOS == "windows" {
					cleanCmd = cmd
				}
				pf.termView.PrintCleanCommand(cleanCmd)
				pf.termView.SetMuted(true)

				activePty.Write([]byte(cmdToWire))
				pf.executing = true
					pf.showPanels = false
				}
			} else {
				if _, isLocal := v.(*vfs.OSVFS); !isLocal {
					vtui.ShowMessage(" Error ", "Cannot execute non-runnable files on a remote file system.", []string{"&Ok"})
					return
				}
				var cmd *exec.Cmd
				switch runtime.GOOS {
				case "linux":
					cmd = exec.Command("xdg-open", path)
				case "windows":
					cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
				case "darwin":
					cmd = exec.Command("open", path)
				}
				if cmd != nil {
					_ = cmd.Start()
				}
			}
		})
	})
}

func actionNewFile(pf *PanelsFrame) {
	if fsp := pf.getActivePanel(); fsp != nil {
		dir := fsp.vfs.GetPath()
		activeVfs := fsp.vfs
		vtui.InputBox(Msg("Edit.NewFileTitle"), Msg("Edit.NewFilePrompt"), "", func(name string) {
			if name == "" {
				name = "newfile.txt"
			}
			actionOpenEditor(pf, activeVfs, activeVfs.Join(dir, name))
		})
	}
}

func actionViewTerminalLog(pf *PanelsFrame) {
	v := &TerminalLogVFS{tv: pf.termView}
	actionOpenViewer(pf, v, "Terminal Log")
}

func actionEditTerminalLog(pf *PanelsFrame) {
	v := &TerminalLogVFS{tv: pf.termView}
	actionOpenEditor(pf, v, "Terminal Log")
}

func actionViewFile(pf *PanelsFrame) {
	if fsp := pf.getActivePanel(); fsp != nil {
		idx := fsp.GetCursorIndex()
		if idx < 0 || idx >= len(fsp.entries) {
			return
		}
		if fsp.entries[idx].IsDir {
			vtui.ShowMessage(" Error ", "Cannot view a directory.", []string{"&Ok"})
			return
		}
		name := fsp.GetSelectedName()
		path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
		actionOpenViewer(pf, fsp.vfs, path)
	}
}

func actionEditFile(pf *PanelsFrame) {
	if fsp := pf.getActivePanel(); fsp != nil {
		idx := fsp.GetCursorIndex()
		if idx < 0 || idx >= len(fsp.entries) {
			return
		}
		if fsp.entries[idx].IsDir {
			vtui.ShowMessage(" Error ", "Cannot edit a directory.", []string{"&Ok"})
			return
		}
		name := fsp.GetSelectedName()
		path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
		actionOpenEditor(pf, fsp.vfs, path)
	}
}

func actionCopyMove(pf *PanelsFrame, isMove bool) {
	fspSrc := pf.getActivePanel()
	fspDst := pf.getInactivePanel()
	if fspSrc == nil || fspDst == nil {
		return
	}

	names := fspSrc.GetSelectedNames()
	if len(names) == 0 {
		return
	}

	title := Msg("Copy.Title")
	prompt := Msg("Copy.Prompt")
	if isMove {
		title = Msg("Move.Title")
		prompt = Msg("Move.Prompt")
	}

	srcVfs, dstVfs := fspSrc.vfs, fspDst.vfs
	dlg := vtui.NewCenteredDialog(50, 11, title)
	dlg.ShowClose = true

	promptLbl := vtui.NewLabel(0, 0, fmt.Sprintf(prompt, len(names)), nil)
	dlg.AddItem(promptLbl)

	initialDest := dstVfs.GetPath()
	if initialDest != "" && !strings.HasSuffix(initialDest, "/") && !strings.HasSuffix(initialDest, "\\") {
		initialDest += string(os.PathSeparator)
	}

	editDest := vtui.NewEdit(0, 0, 10, initialDest)
	dlg.AddItem(editDest)

	chkFork := vtui.NewCheckbox(0, 0, Msg("Op.ClonePanels"), false)
	dlg.AddItem(chkFork)

	btnOk := vtui.NewButton(0, 0, Msg("Copy.Btn"))
	if isMove {
		btnOk = vtui.NewButton(0, 0, Msg("Move.Btn"))
	}
	btnOk.IsDefault = true

	btnOk.OnClick = func() {
		dest := editDest.GetText()
		forked := chkFork.State == 1
		dlg.Close()
		if dest != "" {
			go ExecuteFileOp(pf, srcVfs, dstVfs, names, dest, isMove, forked, pf.RefreshAll)
		}
	}
	dlg.AddItem(btnOk)

	btnCancel := vtui.NewButton(0, 0, "Cancel")
	btnCancel.OnClick = func() { dlg.Close() }
	dlg.AddItem(btnCancel)

	// Layout Engine
	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, 11-4)
	vbox.Add(promptLbl, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(editDest, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Add(chkFork, vtui.Margins{Top: 1}, vtui.AlignLeft)

	hbox := vtui.NewHBoxLayout(0, 0, 50-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnOk, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	editDest.SelectAll()
	dlg.SetFocusedItem(editDest)

	vtui.FrameManager.Push(dlg)
}
func actionEditorSettings(pf *PanelsFrame) {
	dlg := vtui.NewCenteredDialog(54, 11, Msg("EditorSettings.Title"))
	dlg.ShowClose = true

	chkAuto := vtui.NewCheckbox(0, 0, Msg("EditorSettings.AutoComplete"), false)
	chkAuto.State = 0
	if AppConfig.EditorAutoComplete { chkAuto.State = 1 }

	lblMask := vtui.NewLabel(0, 0, Msg("EditorSettings.Mask"), nil)
	editMask := vtui.NewEdit(0, 0, 30, AppConfig.EditorAutoCompleteMask)
	lblMask.FocusLink = editMask

	btnOk := vtui.NewButton(0, 0, Msg("vtui.Ok"))
	btnOk.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, Msg("vtui.Cancel"))

	dlg.AddItem(chkAuto)
	dlg.AddItem(lblMask)
	dlg.AddItem(editMask)
	dlg.AddItem(btnOk)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 54-4, 11-4)
	vbox.Add(chkAuto, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(lblMask, vtui.Margins{Top: 1}, vtui.AlignLeft)
	vbox.Add(editMask, vtui.Margins{}, vtui.AlignFill)

	hbox := vtui.NewHBoxLayout(0, 0, 54-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnOk, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	btnCancel.OnClick = func() { dlg.Close() }
	btnOk.OnClick = func() {
		AppConfig.EditorAutoComplete = chkAuto.State == 1
		AppConfig.EditorAutoCompleteMask = editMask.GetText()
		SaveConfig()
		dlg.Close()
	}

	vtui.FrameManager.Push(dlg)
}

func actionDelete(pf *PanelsFrame) {
	fsp := pf.getActivePanel()
	if fsp == nil {
		return
	}

	activeVfs := fsp.vfs
	names := fsp.GetSelectedNames()
	if len(names) == 0 {
		return
	}

	msgName := names[0]
	if len(names) > 1 {
		msgName = fmt.Sprintf("%d items", len(names))
	}

	title := Msg("Delete.Title")
	msg := fmt.Sprintf(Msg("Delete.Confirm"), msgName)
	lines := vtui.WrapText(msg, 46)

	dlg := vtui.NewCenteredDialog(50, 6+len(lines), title)
	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, (6+len(lines))-4)

	for _, l := range lines {
		t := vtui.NewText(0, 0, l, vtui.Palette[vtui.ColDialogText])
		dlg.AddItem(t)
		vbox.Add(t, vtui.Margins{}, vtui.AlignCenter)
	}

	btnDel := vtui.NewButton(0, 0, Msg("Delete.Btn"))
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	dlg.AddItem(btnDel)
	dlg.AddItem(btnCancel)

	hbox := vtui.NewHBoxLayout(0, 0, 50-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnDel, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	btnCancel.OnClick = func() { dlg.Close() }
	btnDel.OnClick = func() {
		fsp.pendingSelection = fsp.GetSuccessorName()
		dlg.Close()

		opDlg := NewFileOpProgressDialog(" Deleting... ")
		var taskCtx *vtui.TaskContext
		opDlg.btnCancel.OnClick = func() {
			if taskCtx != nil { taskCtx.Cancel() }
			opDlg.Close()
		}

		vtui.FrameManager.PostTask(func() {
			vtui.FrameManager.AddScreenHeadless(opDlg)
		})

		taskCtx = vtui.RunAsync(func(ctx *vtui.TaskContext) {
			defer ctx.RunOnUI(func() {
				opDlg.Close()
				pf.RefreshAll()
			})

			// Pre-scan for progress
			var totalStats vfs.OpStats
			scanErr := error(nil)
			totalStats, scanErr = vfs.CalculateStats(ctx.Context, activeVfs, activeVfs.GetPath(), names, func(currentPath string, stats vfs.OpStats) {
				ctx.RunOnUI(func() {
					opDlg.UpdateScan(currentPath, stats.Files, stats.Dirs)
					vtui.FrameManager.Redraw()
				})
			})

			if scanErr != nil && scanErr != context.Canceled {
				ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to scan files:\n%v", scanErr), []string{"&Ok"}) })
				return
			}
			if ctx.Err() != nil { return }

			tracker := NewFileOpTracker(totalStats)
			lastUpdate := time.Now()

			updateUI := func(force bool) {
				now := time.Now()
				if force || now.Sub(lastUpdate) >= 100*time.Millisecond {
					lastUpdate = now
					filePct, totalPct, currName := tracker.GetProgress()
					processed, total := tracker.GetStats()

					var totalText string
					if total.Bytes > 0 {
						// For delete, bytes deleted isn't super meaningful if it's very fast, but still nice.
						totalText = fmt.Sprintf("Total: %d / %d items", processed.Files+processed.Dirs, total.Files+total.Dirs)
					} else {
						totalText = fmt.Sprintf("Total: %d / %d items", processed.Files+processed.Dirs, total.Files+total.Dirs)
					}

					ctx.RunOnUI(func() {
						opDlg.UpdateTransfer("Deleting", currName, filePct, totalText, totalPct, "")
						vtui.FrameManager.Redraw()
					})
				}
			}

			updateUI(true)

			for _, name := range names {
				if ctx.Err() != nil { return }
				fullPath := activeVfs.Join(activeVfs.GetPath(), name)

				// For delete we should ideally recursively traverse and track each file.
				// But activeVfs.Remove handles recursive delete by itself usually (e.g. os.RemoveAll).
				// So we won't get fine-grained updates unless we do it manually.
				// For now, we just call Remove on the top level and mark it as done.
				// This might jump progress if it's a huge folder.

				tracker.StartFile(name, 0)
				updateUI(true)

				err := activeVfs.Remove(ctx.Context, fullPath)
				if err != nil {
					if err != context.Canceled {
						ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"}) })
					}
					return
				}
				tracker.FileDone()
				updateUI(true)
			}
		})
	}

	vtui.FrameManager.Push(dlg)
}

func actionMkDir(pf *PanelsFrame) {
	panel := pf.getActivePanel()
	if panel == nil {
		return
	}

	activeVfs := panel.vfs

	dlg := vtui.NewCenteredDialog(40, 9, Msg("MakeFolder.Title"))
	dlg.ShowClose = true

	editName := vtui.NewEdit(0, 0, 10, "")
	lblPrompt := vtui.NewLabel(0, 0, Msg("MakeFolder.Prompt"), editName)
	dlg.AddItem(lblPrompt)
	dlg.AddItem(editName)

	btnOk := vtui.NewButton(0, 0, "&Ok")
	btnOk.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	dlg.AddItem(btnOk)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 40-4, 8-4)
	vbox.Add(lblPrompt, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(editName, vtui.Margins{Top: 1}, vtui.AlignFill)

	hbox := vtui.NewHBoxLayout(0, 0, 40-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnOk, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	dlg.SetFocusedItem(editName)

	btnCancel.OnClick = func() { dlg.Close() }
	btnOk.OnClick = func() {
		name := editName.GetText()
		dlg.Close()
		if name == "" {
			return
		}
		fullPath := activeVfs.Join(activeVfs.GetPath(), name)
		vtui.RunAsync(func(ctx *vtui.TaskContext) {
			err := activeVfs.MkDir(ctx.Context, fullPath)
			ctx.RunOnUI(func() {
				if err != nil {
					vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
				}

				// Set pending selection so the panel snaps to the new folder after the async reload
				panel.pendingSelection = name
				pf.RefreshAll()
			})
		})
	}

	vtui.FrameManager.Push(dlg)
}

func actionFindFile(pf *PanelsFrame) {
	activePanel := pf.getActivePanel()
	if activePanel == nil {
		return
	}

	dlg := vtui.NewCenteredDialog(54, 13, Msg("FindFile.Title"))
	dlg.ShowClose = true

	lblMask := vtui.NewLabel(0, 0, Msg("FindFile.MaskPrompt"), nil)
	editMask := vtui.NewEdit(0, 0, 20, LastFindFileMask)
	editMask.SelectAll()
	lblMask.FocusLink = editMask
	dlg.SetFocusedItem(editMask)

	lblText := vtui.NewLabel(0, 0, Msg("FindFile.TextPrompt"), nil)
	editText := vtui.NewEdit(0, 0, 20, LastFindFileText)
	lblText.FocusLink = editText

	btnFind := vtui.NewButton(0, 0, Msg("FindFile.BtnFind"))
	btnFind.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, Msg("vtui.Cancel"))

	dlg.AddItem(lblMask)
	dlg.AddItem(editMask)
	dlg.AddItem(lblText)
	dlg.AddItem(editText)
	dlg.AddItem(btnFind)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 54-4, 13-4)
	vbox.Add(lblMask, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(editMask, vtui.Margins{Top: 1}, vtui.AlignFill)

	vbox.Add(lblText, vtui.Margins{Top: 1}, vtui.AlignLeft)
	vbox.Add(editText, vtui.Margins{Top: 1}, vtui.AlignFill)

	hbox := vtui.NewHBoxLayout(0, 0, 54-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter

	hbox.Spacing = 2
	hbox.Add(btnFind, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	btnCancel.OnClick = func() { dlg.Close() }
	btnFind.OnClick = func() {
		LastFindFileMask = editMask.GetText()
		LastFindFileText = editText.GetText()
		SaveSession()
		dlg.Close()
		if LastFindFileMask != "" {
			ExecuteFindFile(pf, activePanel.vfs, activePanel.vfs.GetPath(), LastFindFileMask, LastFindFileText)
		}
	}

	vtui.FrameManager.Push(dlg)
}
func actionPanelSettings(pf *PanelsFrame) {
	dlg := vtui.NewCenteredDialog(44, 13, Msg("PanelSettings.Title"))
	dlg.ShowClose = true

	chkHidden := vtui.NewCheckbox(0, 0, Msg("PanelSettings.ShowHidden"), false)
	chkHidden.State = 0
	if AppConfig.ShowHiddenFiles { chkHidden.State = 1 }

	chkHighlight := vtui.NewCheckbox(0, 0, Msg("PanelSettings.HighlightDir"), false)
	chkHighlight.State = 0
	if AppConfig.HighlightDir { chkHighlight.State = 1 }

	chkPaths := vtui.NewCheckbox(0, 0, Msg("PanelSettings.SavePaths"), false)
	chkPaths.State = 0
	if AppConfig.SavePanelPaths { chkPaths.State = 1 }

	chkCursor := vtui.NewCheckbox(0, 0, Msg("PanelSettings.KeepCursor"), false)
	chkCursor.State = 0
	if AppConfig.KeepTerminalCursor { chkCursor.State = 1 }

	btnOk := vtui.NewButton(0, 0, Msg("vtui.Ok"))
	btnOk.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, Msg("vtui.Cancel"))

	dlg.AddItem(chkHidden)
	dlg.AddItem(chkHighlight)
	dlg.AddItem(chkPaths)
	dlg.AddItem(chkCursor)
	dlg.AddItem(btnOk)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 44-4, 13-4)
	vbox.Add(chkHidden, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(chkHighlight, vtui.Margins{Top: 1}, vtui.AlignLeft)
	vbox.Add(chkPaths, vtui.Margins{Top: 1}, vtui.AlignLeft)
	vbox.Add(chkCursor, vtui.Margins{Top: 1}, vtui.AlignLeft)

	hbox := vtui.NewHBoxLayout(0, 0, 44-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnOk, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	btnCancel.OnClick = func() { dlg.Close() }
	btnOk.OnClick = func() {
		AppConfig.ShowHiddenFiles = chkHidden.State == 1
		AppConfig.HighlightDir = chkHighlight.State == 1
		AppConfig.SavePanelPaths = chkPaths.State == 1
		AppConfig.KeepTerminalCursor = chkCursor.State == 1
		vtui.ManageCursorStyle = !AppConfig.KeepTerminalCursor
		SaveConfig()
		dlg.Close()
		pf.RefreshAll()
	}

	vtui.FrameManager.Push(dlg)
}

func actionFileAttributes(pf *PanelsFrame) {
	fsp := pf.getActivePanel()
	if fsp == nil { return }
	
	name := fsp.GetSelectedName()
	if name == "" || name == ".." { return }
	
	fullPath := fsp.vfs.Join(fsp.vfs.GetPath(), name)
	
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		item, err := fsp.vfs.Stat(ctx.Context, fullPath)
		ctx.RunOnUI(func() {
			if err != nil {
				vtui.ShowMessage(" Error ", err.Error(), []string{"&Ok"})
				return
			}
			ShowAttributesDialog(pf, fsp.vfs, fullPath, item)
		})
	})
}
