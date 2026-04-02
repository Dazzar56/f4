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
				if err := srcVfs.Rename(ctx.Context, srcPath, destPath); err == nil {
					vtui.DebugLog("FILEOP: Optimized server-side rename: %s -> %s", srcPath, destPath)
					update("", ((i+1)*100)/len(names))
					continue
				}
			}

			err := recursiveCopy(ctx, update, srcVfs, srcPath, dstVfs, destBase, name, state)
			if err != nil { return err }

			if isMove { srcVfs.Remove(ctx.Context, srcPath) }
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

	stat, err := srcVfs.Stat(ctx.Context, srcPath)
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
	dstStat, err := dstVfs.Stat(ctx.Context, destPath)
	exists := err == nil

	if stat.IsDir {
		if !exists {
			if err := dstVfs.MkDir(ctx.Context, destPath); err != nil {
				return err
			}
		} else if !dstStat.IsDir {
			return fmt.Errorf("cannot overwrite file with folder: %s", name)
		}

		var items []vfs.VFSItem
		err := srcVfs.ReadDir(ctx.Context, srcPath, func(chunk []vfs.VFSItem) {
			items = append(items, chunk...)
		})
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
				vtui.DebugLog("FILEOP: User chose OVERWRITE ALL for %s", name)
			case 2:
				return nil // Skip
			case 3:
				vtui.DebugLog("FILEOP: User chose SKIP ALL")
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
		srcFile, err = srcVfs.Open(ctx.Context, srcPath)
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
		dstFile, err = dstVfs.Create(ctx.Context, destPath)
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

	// io.Copy doesn't support context-aware readers, so we implement a simple loop
	buf := make([]byte, 128*1024) // 128KB buffer
	for {
		if ctx.Err() != nil { return ctx.Err() }
		n, rerr := srcFile.Read(ctx.Context, buf)
		if n > 0 {
			if _, werr := dstFile.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF { break }
			return rerr
		}
	}
	return nil
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
