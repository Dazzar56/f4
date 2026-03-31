package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

type FileOpState struct {
	OverwriteAll bool
	SkipAll      bool
}

func ExecuteFileOp(pf *PanelsFrame, srcVfs, dstVfs vfs.VFS, names []string, destBase string, isMove bool, forked bool, onComplete func()) {
	title := " Copying... "
	if isMove {
		title = " Moving... "
	}
	state := &FileOpState{}

	pf.RunProgressTask(title, "Starting...", forked, func(ctx *vtui.TaskContext, update func(msg string, percent int)) error {
		for i, name := range names {
			if ctx.Err() != nil { return ctx.Err() }

			srcPath := srcVfs.Join(srcVfs.GetPath(), name)
			update(fmt.Sprintf("Processing: %s", name), -1)

			if isMove && srcVfs == dstVfs {
				destPath := dstVfs.Join(destBase, name)
				if err := srcVfs.Rename(srcPath, destPath); err == nil {
					update("", ((i+1)*100)/len(names))
					continue
				}
			}

			err := recursiveCopy(ctx, update, srcVfs, srcPath, dstVfs, destBase, name, state)
			if err != nil { return err }

			if isMove { srcVfs.Remove(srcPath) }
			update("", ((i+1)*100)/len(names))
		}
		return nil
	}, func(err error) {
		if err != nil && err != context.Canceled {
			vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
		}
		pf.RefreshAll()
		if onComplete != nil { onComplete() }
	})
}

func recursiveCopy(ctx *vtui.TaskContext, update func(msg string, percent int), srcVfs vfs.VFS, srcPath string, dstVfs vfs.VFS, dstBase, name string, state *FileOpState) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	stat, err := srcVfs.Stat(srcPath)
	if err != nil {
		return err
	}

	destPath := dstVfs.Join(dstBase, name)

	// Robust self-copy protection
	absSrc, errSrc := srcVfs.Abs(srcPath)
	absDst, errDst := dstVfs.Abs(destPath)
	if errSrc != nil || errDst != nil {
		return fmt.Errorf("vfs path error")
	}
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
			if err := dstVfs.MkDir(destPath); err != nil {
				return err
			}
		} else if !dstStat.IsDir {
			return fmt.Errorf("cannot overwrite file with folder: %s", name)
		}

		items, err := srcVfs.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Name == ".." {
				continue
			}
			if err := recursiveCopy(ctx, update, srcVfs, srcVfs.Join(srcPath, item.Name), dstVfs, destPath, item.Name, state); err != nil {
				return err
			}
		}
		return nil
	}

	// Copy file
	update(fmt.Sprintf("Copying: %s", name), -1)

	if exists {
		if dstStat.IsDir {
			return fmt.Errorf("cannot overwrite folder with file: %s", name)
		}
		if state.SkipAll {
			return nil
		}
		if !state.OverwriteAll {
			choice := AskOverwrite(ctx, name)
			switch choice {
			case 1:
				state.OverwriteAll = true
			case 2:
				return nil // Skip
			case 3:
				state.SkipAll = true
				return nil
			case 4:
				return context.Canceled // Cancel
			}
		}
	}

	var srcFile vfs.ReadAtCloser
	var dstFile io.WriteCloser

	// Open Source with Retry
	for {
		srcFile, err = srcVfs.Open(srcPath)
		if err == nil {
			break
		}
		choice := AskError(ctx, "Cannot open source file", err)
		if choice == 1 {
			return nil
		} // Skip
		if choice == 2 {
			return context.Canceled
		} // Abort
	}
	defer srcFile.Close()

	// Create Destination with Retry
	for {
		dstFile, err = dstVfs.Create(destPath)
		if err == nil {
			break
		}
		choice := AskError(ctx, "Cannot create destination file", err)
		if choice == 1 {
			return nil
		} // Skip
		if choice == 2 {
			return context.Canceled
		} // Abort
	}
	defer dstFile.Close()

	// Simple byte-by-byte copy (can be optimized with buffers)
	_, err = io.Copy(dstFile, srcFile)
	return err
}

// AskOverwrite shows a modal dialog from the background thread and waits for the result.
func AskOverwrite(ctx *vtui.TaskContext, name string) int {
	resultChan := make(chan int, 1)

	ctx.RunOnUI(func() {
		msg := fmt.Sprintf("File already exists:\n%s\n\nOverwrite?", name)
		title := " Conflict "
		buttons := []string{"&Overwrite", Msg("Btn.OverwriteAll"), "&Skip", Msg("Btn.SkipAll"), "&Cancel"}

		dlg := vtui.ShowMessage(title, msg, buttons)
		dlg.OnResult = func(code int) {
			if code < 0 {
				code = 4
			} // Map ESC/Close to Cancel
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
func AskError(ctx *vtui.TaskContext, op string, err error) int {
	resultChan := make(chan int, 1)
	ctx.RunOnUI(func() {
		msg := fmt.Sprintf("%s:\n%s\n\n%s", op, err.Error(), "What to do?")
		dlg := vtui.ShowMessage(" Error ", msg, []string{Msg("Btn.Retry"), "&Skip", "&Abort"})
		dlg.OnResult = func(code int) {
			if code < 0 {
				code = 2
			}
			resultChan <- code
		}
	})
	select {
	case res := <-resultChan:
		return res
	case <-ctx.Done():
		return 2
	}
}
