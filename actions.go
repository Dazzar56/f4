package main

import (
	"context"
	"os"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"path/filepath"
	"io"

	"github.com/unxed/f4/vfs"
	"github.com/mholt/archives"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

func actionOpenEditor(pf *PanelsFrame, v vfs.VFS, path string) {
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		var f vfs.ReadAtCloser
		var pt *piecetable.PieceTable
		var buf *AsyncBuffer

		if v != nil {
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
		viewer, err := NewViewerView(ctx.Context, v, path)
		ctx.RunOnUI(func() {
			if err != nil {
				vtui.DebugLog("PANELS: Failed to open viewer for %s: %v", path, err)
				vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to open file:\n%v", err), []string{"&Ok"})
				return
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
					activePty.Write([]byte(fmt.Sprintf(" cd %q\r", dir)))
					cmd := name
					if runtime.GOOS != "windows" { cmd = "./" + name }
					activePty.Write([]byte(cmd + "\r"))
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

func actionViewFile(pf *PanelsFrame) {
	if fsp := pf.getActivePanel(); fsp != nil {
		name := fsp.GetSelectedName()
		path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
		actionOpenViewer(pf, fsp.vfs, path)
	}
}

func actionEditFile(pf *PanelsFrame) {
	if fsp := pf.getActivePanel(); fsp != nil {
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

	editDest := vtui.NewEdit(0, 0, 10, dstVfs.GetPath())
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

func actionMkDir(pf *PanelsFrame) {
	panel := pf.getActivePanel()
	if panel == nil {
		return
	}

	activeVfs := panel.vfs
	if nf, isNetFox := activeVfs.(*vfs.NetFoxVFS); isNetFox {
		actionNewConnection(pf, nf)
		return
	}

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

func actionNewConnection(pf *PanelsFrame, nf *vfs.NetFoxVFS) {
	dlg := vtui.NewCenteredDialog(48, 14, " SFTP Connection ")
	dlg.ShowClose = true

	lblName := vtui.NewLabel(0, 0, "Connection &Name:", nil)
	editName := vtui.NewEdit(0, 0, 20, "MyServer")
	lblName.FocusLink = editName

	lblHost := vtui.NewLabel(0, 0, "&Host or IP:", nil)
	editHost := vtui.NewEdit(0, 0, 30, "")
	lblHost.FocusLink = editHost

	lblPort := vtui.NewLabel(0, 0, "&Port:", nil)
	editPort := vtui.NewEdit(0, 0, 6, "22")
	lblPort.FocusLink = editPort

	lblUser := vtui.NewLabel(0, 0, "&User:", nil)
	editUser := vtui.NewEdit(0, 0, 20, "root")
	lblUser.FocusLink = editUser

	lblPass := vtui.NewLabel(0, 0, "Pass&word:", nil)
	editPass := vtui.NewEdit(0, 0, 20, "") // Используем NewEdit вместо NewPswEdit
	lblPass.FocusLink = editPass

	btnOk := vtui.NewButton(0, 0, "&Ok")
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	btnOk.IsDefault = true

	dlg.AddItem(lblName); dlg.AddItem(editName)
	dlg.AddItem(lblHost); dlg.AddItem(editHost)
	dlg.AddItem(lblPort); dlg.AddItem(editPort)
	dlg.AddItem(lblUser); dlg.AddItem(editUser)
	dlg.AddItem(lblPass); dlg.AddItem(editPass)
	dlg.AddItem(btnOk); dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+1, 48-4, 14-2)
	vbox.Add(lblName, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(editName, vtui.Margins{}, vtui.AlignFill)

	hbox1 := vtui.NewHBoxLayout(0, 0, 48-4, 2)
	vboxHost := vtui.NewVBoxLayout(0, 0, 30, 2)
	vboxHost.Add(lblHost, vtui.Margins{}, vtui.AlignLeft); vboxHost.Add(editHost, vtui.Margins{}, vtui.AlignFill)
	vboxPort := vtui.NewVBoxLayout(0, 0, 10, 2)
	vboxPort.Add(lblPort, vtui.Margins{}, vtui.AlignLeft); vboxPort.Add(editPort, vtui.Margins{}, vtui.AlignFill)
	hbox1.Add(vboxHost, vtui.Margins{Right: 2}, vtui.AlignLeft); hbox1.Add(vboxPort, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(hbox1, vtui.Margins{Top: 1}, vtui.AlignFill)

	hbox2 := vtui.NewHBoxLayout(0, 0, 48-4, 2)
	vboxUser := vtui.NewVBoxLayout(0, 0, 20, 2)
	vboxUser.Add(lblUser, vtui.Margins{}, vtui.AlignLeft); vboxUser.Add(editUser, vtui.Margins{}, vtui.AlignFill)
	vboxPass := vtui.NewVBoxLayout(0, 0, 20, 2)
	vboxPass.Add(lblPass, vtui.Margins{}, vtui.AlignLeft); vboxPass.Add(editPass, vtui.Margins{}, vtui.AlignFill)
	hbox2.Add(vboxUser, vtui.Margins{Right: 2}, vtui.AlignLeft); hbox2.Add(vboxPass, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(hbox2, vtui.Margins{Top: 1}, vtui.AlignFill)

	hboxBtns := vtui.NewHBoxLayout(0, 0, 48-4, 1)
	hboxBtns.HorizontalAlign = vtui.AlignCenter; hboxBtns.Spacing = 2
	hboxBtns.Add(btnOk, vtui.Margins{}, vtui.AlignTop); hboxBtns.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	vbox.Add(hboxBtns, vtui.Margins{Top: 1}, vtui.AlignFill)

	vbox.Apply()
	dlg.SetFocusedItem(editName)

	btnCancel.OnClick = func() { dlg.Close() }
	btnOk.OnClick = func() {
		name := editName.GetText()
		if name != "" {
			nf.SaveConfig(name, vfs.NetFoxConfig{
				Host: editHost.GetText(),
				Port: editPort.GetText(),
				User: editUser.GetText(),
				Pass: editPass.GetText(),
			})
			pf.RefreshAll()
		}
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
		pf.RunProgressTask(" Deleting... ", "Preparing...", false, func(ctx *vtui.TaskContext, update func(msg string, percent int)) error {
			for i, name := range names {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				update(fmt.Sprintf("Deleting: %s", name), (i*100)/len(names))
				fullPath := activeVfs.Join(activeVfs.GetPath(), name)
				if err := activeVfs.Remove(ctx.Context, fullPath); err != nil {
					return err
				}
			}
			return nil
		}, func(err error) {
			if err != nil && err != context.Canceled {
				vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
			}
			pf.RefreshAll()
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
	editMask := vtui.NewEdit(0, 0, 20, "*")
	lblMask.FocusLink = editMask

	lblText := vtui.NewLabel(0, 0, Msg("FindFile.TextPrompt"), nil)
	editText := vtui.NewEdit(0, 0, 20, "")
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
		mask := editMask.GetText()
		text := editText.GetText()
		dlg.Close()
		if mask != "" {
			ExecuteFindFile(pf, activePanel.vfs, activePanel.vfs.GetPath(), mask, text)
		}
	}

	vtui.FrameManager.Push(dlg)
}

func actionArchiveCommands(pf *PanelsFrame) {
	panel := pf.getActivePanel()
	if panel == nil { return }

	menu := vtui.NewVMenu(" Archive Commands ")
	menu.AddItem(vtui.MenuItem{Text: "&1. Add to archive"})
	menu.AddItem(vtui.MenuItem{Text: "&2. Extract files"})

	// Задаем размеры: ширина 30, высота = кол-во пунктов + 2 (рамки)
	w, h := 30, menu.GetItemCount()+2
	x := (pf.lastW - w) / 2
	y := (pf.lastH - h) / 2
	menu.SetPosition(x, y, x+w-1, y+h-1)

	menu.OnAction = func(idx int) {
		menu.Close()
		switch idx {
		case 0:
			actionAddArchive(pf)
		case 1:
			actionExtractArchive(pf)
		}
	}

	vtui.FrameManager.Push(menu)
}

func actionExtractArchive(pf *PanelsFrame) {
	srcPanel := pf.getActivePanel()
	dstPanel := pf.getInactivePanel()
	if srcPanel == nil || dstPanel == nil { return }

	name := srcPanel.GetSelectedName()
	if name == "" || name == ".." { return }

	srcPath := srcPanel.vfs.Join(srcPanel.vfs.GetPath(), name)
	destDir := dstPanel.vfs.GetPath()

	pf.RunProgressTask(" Extracting... ", "Identifying archive...", false, func(tctx *vtui.TaskContext, update func(msg string, percent int)) error {
		f, err := os.Open(srcPath)
		if err != nil { return err }
		defer f.Close()

		format, stream, err := archives.Identify(tctx.Context, srcPath, f)
		if err != nil { return err }

		ex, ok := format.(archives.Extractor)
		if !ok { return fmt.Errorf("file is not an extractable archive") }

		// Подсчитываем файлы для прогресс-бара (опционально, но полезно)
		return ex.Extract(tctx.Context, stream, func(ctx context.Context, info archives.FileInfo) error {
			if tctx.Err() != nil { return tctx.Err() }

			update(fmt.Sprintf("Extracting: %s", info.NameInArchive), -1)

			targetPath := filepath.Join(destDir, info.NameInArchive)

			if info.IsDir() {
				return os.MkdirAll(targetPath, 0755)
			}

			// Создаем подпапки если их нет
			os.MkdirAll(filepath.Dir(targetPath), 0755)

			out, err := os.Create(targetPath)
			if err != nil { return err }
			defer out.Close()

			in, err := info.Open()
			if err != nil { return err }
			defer in.Close()

			_, err = io.Copy(out, in)
			return err
		})
	}, func(err error) {
		if err != nil && err != context.Canceled {
			vtui.ShowMessage(" Error ", fmt.Sprintf("Extraction failed:\n%v", err), []string{"&Ok"})
		}
		pf.RefreshAll()
	})
}

func actionAddArchive(pf *PanelsFrame) {
	panel := pf.getActivePanel()
	if panel == nil { return }

	names := panel.GetSelectedNames()
	if len(names) == 0 { return }

	// Default archive name
	arcName := panel.vfs.Base(panel.vfs.GetPath())
	if arcName == "." || arcName == "" { arcName = "archive" }
	arcName += ".zip"

	vtui.InputBox(" Add to archive ", "Archive name:", arcName, func(name string) {
		if name == "" { return }

		fullArcPath := panel.vfs.Join(panel.vfs.GetPath(), name)

		pf.RunProgressTask(" Archiving... ", "Gathering files...", false, func(tctx *vtui.TaskContext, update func(msg string, percent int)) error {
			// Convert names to archives.FileInfo
			var files []archives.FileInfo
			for i, n := range names {
				if tctx.Err() != nil { return tctx.Err() }
				update(fmt.Sprintf("Scanning: %s", n), (i*100)/len(names))

				fullPath := panel.vfs.Join(panel.vfs.GetPath(), n)

				// For OSVFS we can use archives.FilesFromDisk
				if osvfs, ok := panel.vfs.(*vfs.OSVFS); ok {
					absPath, _ := osvfs.Abs(fullPath)
					moreFiles, err := archives.FilesFromDisk(tctx.Context, nil, map[string]string{absPath: n})
					if err == nil {
						files = append(files, moreFiles...)
					}
				}
			}

			out, err := os.Create(fullArcPath)
			if err != nil { return err }
			defer out.Close()

			format := archives.Zip{}
			// In archives library, Archive is a method that takes a writer and slice of FileInfo
			return format.Archive(tctx.Context, out, files)

		}, func(err error) {
			if err != nil && err != context.Canceled {
				vtui.ShowMessage(" Error ", fmt.Sprintf("Archiving failed:\n%v", err), []string{"&Ok"})
			}
			pf.RefreshAll()
		})
	})
}
