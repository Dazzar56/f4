package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

func actionOpenEditor(pf *PanelsFrame, v vfs.VFS, path string) {
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		var f vfs.ReadAtCloser
		var pt *piecetable.PieceTable
		if v != nil {
			f, _ = v.Open(ctx.Context, path)
		}
		if ctx.Err() != nil {
			if f != nil { f.Close() }
			return
		}
		if f != nil {
			pt = piecetable.NewWithBuffer(NewFileBuffer(f))
		} else {
			pt = piecetable.New(nil)
		}

		ctx.RunOnUI(func() {
			editor := NewEditorView(pt, v, path)
			editor.SetFile(f)
			editor.ResizeConsole(pf.lastW, pf.lastH)
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

func actionExecute(pf *PanelsFrame, v vfs.VFS, dir, name, path string) {
	if _, isLocal := v.(*vfs.OSVFS); !isLocal {
		vtui.ShowMessage(" Error ", "Cannot execute files on a remote file system.", []string{"&Ok"})
		return
	}

	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		runnable := vfs.IsTerminalRunnable(ctx.Context, v, path)
		ctx.RunOnUI(func() {
			if runnable {
				if pf.pty != nil {
					pf.pty.Write([]byte(fmt.Sprintf(" cd %q\r", dir)))
					cmd := name
					if runtime.GOOS != "windows" {
						cmd = "./" + name
					}
					pf.pty.Write([]byte(cmd + "\r"))
				}
				pf.showPanels = false
			} else {
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

	dlg.AddItem(vtui.NewLabel(dlg.X1+2, dlg.Y1+2, fmt.Sprintf(prompt, len(names)), nil))
	editDest := vtui.NewEdit(dlg.X1+2, dlg.Y1+3, 46, dstVfs.GetPath())
	dlg.AddItem(editDest)

	chkFork := vtui.NewCheckbox(dlg.X1+2, dlg.Y1+5, Msg("Op.ClonePanels"), false)
	dlg.AddItem(chkFork)

	btnOk := vtui.NewButton(dlg.X1+10, dlg.Y1+8, Msg("Copy.Btn"))
	if isMove {
		btnOk = vtui.NewButton(dlg.X1+10, dlg.Y1+8, Msg("Move.Btn"))
	}

	btnOk.OnClick = func() {
		dest := editDest.GetText()
		forked := chkFork.State == 1
		dlg.Close()
		if dest != "" {
			go ExecuteFileOp(pf, srcVfs, dstVfs, names, dest, isMove, forked, pf.RefreshAll)
		}
	}
	dlg.AddItem(btnOk)

	btnCancel := vtui.NewButton(dlg.X1+25, dlg.Y1+8, "Cancel")
	btnCancel.OnClick = func() { dlg.Close() }
	dlg.AddItem(btnCancel)

	vtui.FrameManager.Push(dlg)
}

func actionMkDir(pf *PanelsFrame) {
	panel := pf.getActivePanel()
	if panel == nil {
		return
	}

	activeVfs := panel.vfs

	vtui.InputBox(Msg("MakeFolder.Title"), Msg("MakeFolder.Prompt"), "", func(name string) {
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
				pf.RefreshAll()
				panel.SelectName(name)
			})
		})
	})
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

	msg := fmt.Sprintf(Msg("Delete.Confirm"), msgName)
	dlg := vtui.ShowMessage(Msg("Delete.Title"), msg, []string{Msg("Delete.Btn"), "Cancel"})
	dlg.OnResult = func(code int) {
		if code == 0 {
			pf.RunProgressTask(" Deleting... ", "Preparing...", false, func(ctx *vtui.TaskContext, update func(msg string, percent int)) error {
				for i, name := range names {
					if ctx.Err() != nil { return ctx.Err() }
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
	}
}