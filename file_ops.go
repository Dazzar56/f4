package main

import (
	"context"
	"strings"
	"os"
	"fmt"
	"io"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

type FileOpState struct {
	OverwriteAll bool
	SkipAll      bool
}

func (pf *PanelsFrame) ExecuteFileOp(srcVfs, dstVfs vfs.VFS, name, destBase string, isMove bool) {
	title := " Copying... "
	if isMove { title = " Moving... " }
	state := &FileOpState{}

	dlg := vtui.NewDialog(0, 0, 50, 8, title)
	dlg.Center(vtui.FrameManager.GetScreenSize(), 25)
	lbl := vtui.NewText(dlg.X1+2, dlg.Y1+2, "Starting...", vtui.Palette[vtui.ColDialogText])
	dlg.AddItem(lbl)

	btnCancel := vtui.NewButton(dlg.X1+20, dlg.Y1+5, "&Cancel")
	var taskCtx *vtui.TaskContext
	btnCancel.OnClick = func() { if taskCtx != nil { taskCtx.Cancel() }; dlg.Close() }
	dlg.AddItem(btnCancel)

	vtui.FrameManager.PostTask(func() { vtui.FrameManager.Push(dlg) })

	taskCtx = vtui.RunAsync(func(ctx *vtui.TaskContext) {
		srcPath := srcVfs.Join(srcVfs.GetPath(), name)

		// If move is on the same VFS, try Rename first
		if isMove && srcVfs == dstVfs {
			destPath := dstVfs.Join(destBase, name)
			if err := srcVfs.Rename(srcPath, destPath); err == nil {
				ctx.RunOnUI(func() { dlg.Close(); pf.RefreshAll() })
				return
			}
		}

		// Fallback to recursive copy
		// Fallback to recursive copy
		err := pf.recursiveCopy(ctx, dlg, lbl, srcVfs, srcPath, dstVfs, destBase, name, state)

		if isMove && err == nil {
			err = srcVfs.Remove(srcPath)
		}

		ctx.RunOnUI(func() {
			dlg.Close()
			if err != nil {
				vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
			}
			pf.RefreshAll()
		})
	})
}

func (pf *PanelsFrame) recursiveCopy(ctx *vtui.TaskContext, dlg *vtui.Dialog, lbl *vtui.Text, srcVfs vfs.VFS, srcPath string, dstVfs vfs.VFS, dstBase, name string, state *FileOpState) error {
	if ctx.Err() != nil { return ctx.Err() }

	stat, err := srcVfs.Stat(srcPath)
	if err != nil { return err }

	destPath := dstVfs.Join(dstBase, name)
	
	// Robust self-copy protection
	absSrc, _ := srcVfs.Abs(srcPath)
	absDst, _ := dstVfs.Abs(destPath)
	if absSrc == absDst {
		return fmt.Errorf("cannot copy folder into itself (source equals destination)")
	}
	if strings.HasPrefix(absDst, absSrc+string(os.PathSeparator)) {
		return fmt.Errorf("cannot copy folder into itself (destination is a subfolder)")
	}

	// Check if destination already exists
	dstStat, err := dstVfs.Stat(destPath)
	exists := err == nil

	if stat.IsDir {
		if !exists {
			if err := dstVfs.MkDir(destPath); err != nil { return err }
		} else if !dstStat.IsDir {
			return fmt.Errorf("cannot overwrite file with folder: %s", name)
		}

		items, err := srcVfs.ReadDir(srcPath)
		if err != nil { return err }
		for _, item := range items {
			if item.Name == ".." { continue }
			if err := pf.recursiveCopy(ctx, dlg, lbl, srcVfs, srcVfs.Join(srcPath, item.Name), dstVfs, destPath, item.Name, state); err != nil {
				return err
			}
		}
		return nil
	}

	// Copy file
	ctx.RunOnUI(func() { lbl.SetText(fmt.Sprintf("Copying: %s", name)) })

	// Copy file
	if exists {
		if dstStat.IsDir {
			return fmt.Errorf("cannot overwrite folder with file: %s", name)
		}
		if state.SkipAll { return nil }
		if !state.OverwriteAll {
			choice := pf.AskOverwrite(ctx, name)
			switch choice {
			case 1: state.OverwriteAll = true
			case 2: return nil // Skip
			case 3: state.SkipAll = true; return nil
			case 4: return context.Canceled // Cancel
			}
		}
	}

	ctx.RunOnUI(func() { lbl.SetText(fmt.Sprintf("Copying: %s", name)) })

	var srcFile vfs.ReadAtCloser
	var dstFile io.WriteCloser

	// Open Source with Retry
	for {
		srcFile, err = srcVfs.Open(srcPath)
		if err == nil { break }
		choice := pf.AskError(ctx, "Cannot open source file", err)
		if choice == 1 { return nil } // Skip
		if choice == 2 { return context.Canceled } // Abort
	}
	defer srcFile.Close()

	// Create Destination with Retry
	for {
		dstFile, err = dstVfs.Create(destPath)
		if err == nil { break }
		choice := pf.AskError(ctx, "Cannot create destination file", err)
		if choice == 1 { return nil } // Skip
		if choice == 2 { return context.Canceled } // Abort
	}
	defer dstFile.Close()

	// Simple byte-by-byte copy (can be optimized with buffers)
	_, err = io.Copy(dstFile, srcFile)
	return err
}

// AskOverwrite shows a modal dialog from the background thread and waits for the result.
func (pf *PanelsFrame) AskOverwrite(ctx *vtui.TaskContext, name string) int {
	resultChan := make(chan int, 1)
	
	ctx.RunOnUI(func() {
		msg := fmt.Sprintf("File already exists:\n%s\n\nOverwrite?", name)
		title := " Conflict "
		buttons := []string{"&Overwrite", Msg("Btn.OverwriteAll"), "&Skip", Msg("Btn.SkipAll"), "&Cancel"}

		dlg := vtui.ShowMessage(title, msg, buttons)
		dlg.OnResult = func(code int) {
			if code < 0 { code = 4 } // Map ESC/Close to Cancel
			resultChan <- code
		}
	})

	select {
	case res := <-resultChan:
		return res
	case <-ctx.Done():
		return 2 // Cancel if task is killed
	}
}

// AskError handles I/O errors by asking user for Retry/Skip/Abort
func (pf *PanelsFrame) AskError(ctx *vtui.TaskContext, op string, err error) int {
	resultChan := make(chan int, 1)
	ctx.RunOnUI(func() {
		msg := fmt.Sprintf("%s:\n%s\n\n%s", op, err.Error(), "What to do?")
		dlg := vtui.ShowMessage(" Error ", msg, []string{Msg("Btn.Retry"), "&Skip", "&Abort"})
		dlg.OnResult = func(code int) {
			if code < 0 { code = 2 }
			resultChan <- code
		}
	})
	select {
	case res := <-resultChan: return res
	case <-ctx.Done(): return 2
	}
}

