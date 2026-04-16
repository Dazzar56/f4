package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

type FileOpState struct {
	OverwriteAll bool
	SkipAll      bool
	SkippedCount int
	OnBytes      func(int)
	Tracker      *FileOpTracker
	UpdateUI     func(force bool)
}

// formatSize formats a byte count into a human-readable string.
func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func ExecuteFileOp(pf *PanelsFrame, srcVfs, dstVfs vfs.VFS, names []string, destInput string, isMove bool, forked bool, onComplete func()) {
	title := " Copying... "
	if isMove {
		title = " Moving... "
	}

	dlg := NewFileOpProgressDialog(title)

	var taskCtx *vtui.TaskContext
	dlg.btnCancel.OnClick = func() {
		dlg.SetExitCode(1)
	}
	dlg.OnResult = func(code int) {
		if taskCtx != nil {
			taskCtx.Cancel()
		}
	}

	vtui.FrameManager.PostTask(func() {
		if forked && pf != nil {
			clone := pf.Clone()
			vtui.FrameManager.AddScreen(clone)
			vtui.FrameManager.Push(dlg)
		} else {
			vtui.FrameManager.AddScreenHeadless(dlg)
		}
	})

	taskCtx = vtui.RunAsync(func(ctx *vtui.TaskContext) {
		defer ctx.RunOnUI(func() {
			dlg.Close()
			if pf != nil {
				pf.RefreshAll()
			}
			if onComplete != nil {
				onComplete()
			}
		})

		destPath := destInput
		if !filepath.IsAbs(destPath) {
			if !strings.ContainsAny(destInput, "/\\") && destInput != "." && destInput != ".." {
				destPath = srcVfs.Join(srcVfs.GetPath(), destInput)
				dstVfs = srcVfs
			} else {
				destPath = dstVfs.Join(dstVfs.GetPath(), destPath)
			}
		}

		isTargetDir := len(names) > 1
		if !isTargetDir {
			if strings.HasSuffix(destInput, "/") || strings.HasSuffix(destInput, "\\") {
				isTargetDir = true
			} else if stat, err := dstVfs.Stat(ctx.Context, destPath); err == nil && stat.IsDir {
				isTargetDir = true
			} else if destInput == "." || destInput == ".." {
				isTargetDir = true
			}
		}

		if isMove && pf != nil {
			if fspSrc := pf.getActivePanel(); fspSrc != nil {
				fspSrc.pendingSelection = fspSrc.GetSuccessorName()
			}
		}

		dirToEnsure := destPath
		if !isTargetDir {
			dirToEnsure = dstVfs.Dir(destPath)
		}

		if dirToEnsure != "" && dirToEnsure != "." {
			st, err := dstVfs.Stat(ctx.Context, dirToEnsure)
			if err != nil {
				if mkErr := dstVfs.MkDir(ctx.Context, dirToEnsure); mkErr != nil {
					ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), mkErr.Error()), []string{"&Ok"}) })
					return
				}
			} else if !st.IsDir {
				ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf("Target path component is not a directory: %s", dirToEnsure), []string{"&Ok"}) })
				return
			}
		}

		var totalStats vfs.OpStats
		scanErr := error(nil)
		lastScanUpdate := time.Now()
		totalStats, scanErr = vfs.CalculateStats(ctx.Context, srcVfs, srcVfs.GetPath(), names, func(currentPath string, stats vfs.OpStats) {
			now := time.Now()
			if now.Sub(lastScanUpdate) > 50*time.Millisecond {
				lastScanUpdate = now
				ctx.RunOnUI(func() {
					dlg.UpdateScan(currentPath, stats.Files, stats.Dirs)
					vtui.FrameManager.Redraw()
				})
			}
		})
		ctx.RunOnUI(func() {
			dlg.UpdateScan("", totalStats.Files, totalStats.Dirs)
			vtui.FrameManager.Redraw()
		})

		if scanErr != nil {
			if scanErr != context.Canceled {
				ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to scan files:\n%v", scanErr), []string{"&Ok"}) })
			}
			return
		}
		if ctx.Err() != nil { return }

		tracker := NewFileOpTracker(totalStats)

		startTime := time.Now()
		lastUpdate := startTime
		lastSpeedUpdate := startTime
		bytesSinceLastSpeedUpdate := int64(0)
		currentSpeed := float64(0)

		lastLoggedTime := startTime
		lastLoggedPct := -1

		updateUI := func(force bool) {
			now := time.Now()
			if force || now.Sub(lastUpdate) >= 100*time.Millisecond {
				speedDur := now.Sub(lastSpeedUpdate).Seconds()
				if speedDur >= 1.0 {
					currentSpeed = float64(bytesSinceLastSpeedUpdate) / speedDur
					lastSpeedUpdate = now
					bytesSinceLastSpeedUpdate = 0
				}
				lastUpdate = now

				filePct, totalPct, currName := tracker.GetProgress()
				processed, total := tracker.GetStats()

				var totalText string
				if total.Bytes > 0 {
					totalText = fmt.Sprintf("Total: %s / %s", formatSize(processed.Bytes), formatSize(total.Bytes))
				} else {
					totalText = fmt.Sprintf("Total: %d / %d items", processed.Files+processed.Dirs, total.Files+total.Dirs)
				}

				elapsed := now.Sub(startTime)
				elapsedStr := fmt.Sprintf("Time: %02d:%02d:%02d", int(elapsed.Hours()), int(elapsed.Minutes())%60, int(elapsed.Seconds())%60)

				// "Adult" ETA: Account for per-item overhead (metadata, open/close, latency)
				// 32KB is a reasonable virtual 'weight' for one file operation.
				const ItemOverhead = 32 * 1024
				vProcessed := float64(processed.Bytes + (processed.Files+processed.Dirs)*ItemOverhead)
				vTotal := float64(total.Bytes + (total.Files+total.Dirs)*ItemOverhead)

				etaStr := "Remaining: ??:??:??"
				// Use total average virtual speed for maximum ETA stability
				if vTotal > 0 && vProcessed > 0 && elapsed.Seconds() > 0.5 {
					ratio := vProcessed / vTotal
					etaSecs := (elapsed.Seconds() / ratio) - elapsed.Seconds()
					if etaSecs < 0 { etaSecs = 0 }
					etaDur := time.Duration(etaSecs * float64(time.Second))
					etaStr = fmt.Sprintf("Remaining: %02d:%02d:%02d", int(etaDur.Hours()), int(etaDur.Minutes())%60, int(etaDur.Seconds())%60)
				}

				speedStr := ""
				if currentSpeed > 0 {
					speedStr = formatSize(int64(currentSpeed)) + "/s"
				}

				// Форматируем строку: 16 символов, 21 символ, 15 символов -> ровно 52 символа + пробелы = 54 (внутренняя ширина диалога 60-6)
				timeSpeedText := fmt.Sprintf("%-16s %-21s %15s", elapsedStr, etaStr, speedStr)

				// Calculate virtual percentage for debugging/testing
				vPct := 0
				if vTotal > 0 {
					vPct = int((vProcessed * 100) / vTotal)
				}

				// Log progress to debug (with 5% or 5s debounce)
				if totalPct >= lastLoggedPct + 5 || now.Sub(lastLoggedTime) >= 5*time.Second {
					vtui.DebugLog("FILEOP: %d%% (V:%d%%) | Items: %d/%d | Proc: %d/%d B | %s | %s | %s",
						totalPct, vPct,
						processed.Files+processed.Dirs, total.Files+total.Dirs,
						processed.Bytes, total.Bytes,
						strings.TrimSpace(elapsedStr), strings.TrimSpace(etaStr), strings.TrimSpace(speedStr))
					lastLoggedPct = totalPct
					lastLoggedTime = now
				}

				action := "Copying"
				if isMove { action = "Moving" }

				ctx.RunOnUI(func() {
					dlg.UpdateTransfer(action, currName, filePct, totalText, totalPct, timeSpeedText)
					vtui.FrameManager.Redraw()
				})
			}
		}

		state := &FileOpState{
			Tracker: tracker,
			UpdateUI: updateUI,
			OnBytes: func(n int) {
				tracker.UpdateBytes(n)
				bytesSinceLastSpeedUpdate += int64(n)
				updateUI(false)
			},
		}

		updateUI(true)

		for _, name := range names {
			if ctx.Err() != nil { return }

			srcPath := srcVfs.Join(srcVfs.GetPath(), name)
			targetItemPath := destPath
			if isTargetDir {
				targetItemPath = dstVfs.Join(destPath, name)
			}

			if isMove && srcVfs == dstVfs {
				if _, err := dstVfs.Stat(ctx.Context, targetItemPath); err != nil {
					if err := srcVfs.Rename(ctx.Context, srcPath, targetItemPath); err == nil {
						vtui.DebugLog("FILEOP: Optimized server-side rename: %s -> %s", srcPath, targetItemPath)

						itemStat, _ := dstVfs.Stat(ctx.Context, targetItemPath)
						if itemStat.IsDir {
							tracker.DirDone()
						} else {
							tracker.StartFile(name, itemStat.Size)
							tracker.UpdateBytes(int(itemStat.Size))
							tracker.FileDone()
						}
						updateUI(true)
						continue
					}
				}
			}

			err := recursiveCopy(ctx.Context, srcVfs, srcPath, dstVfs, targetItemPath, state, 0)
			if err != nil {
				if err != context.Canceled {
					ctx.RunOnUI(func() { vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"}) })
				}
				return
			}

			if isMove && state.SkippedCount == 0 {
				srcVfs.Remove(ctx.Context, srcPath)
			}
			updateUI(true)
		}
	})
}

func recursiveCopy(ctx context.Context, srcVfs vfs.VFS, srcPath string, dstVfs vfs.VFS, destPath string, state *FileOpState, depth int) error {
	if depth > 1000 {
		return fmt.Errorf("maximum recursion depth exceeded (circular structure?)")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	stat, err := srcVfs.Stat(ctx, srcPath)
	if err != nil {
		return err
	}

	absSrc, _ := srcVfs.Abs(srcPath)
	absDst, _ := dstVfs.Abs(destPath)

	realSrc := absSrc
	realDst := absDst

	if _, ok := srcVfs.(*vfs.OSVFS); ok {
		if resolved, err := filepath.EvalSymlinks(absSrc); err == nil {
			realSrc = resolved
		}
	}
	if _, ok := dstVfs.(*vfs.OSVFS); ok {
		if resolved, err := filepath.EvalSymlinks(absDst); err == nil {
			realDst = resolved
		}
	}

	if realSrc == realDst {
		return fmt.Errorf("cannot copy folder into itself (source equals destination)")
	}

	sep := string(os.PathSeparator)
	if !strings.HasSuffix(realSrc, sep) {
		realSrc += sep
	}

	if strings.HasPrefix(realDst, realSrc) {
		return fmt.Errorf("cannot copy folder into itself (destination is a subfolder)")
	}

	dstStat, err := dstVfs.Stat(ctx, destPath)
	exists := err == nil

	if stat.IsDir {
		if !exists {
			if err := dstVfs.MkDir(ctx, destPath); err != nil {
				return err
			}
		} else if !dstStat.IsDir {
			return fmt.Errorf("cannot overwrite file with folder: %s", dstVfs.Base(destPath))
		}

		var items []vfs.VFSItem
		err := srcVfs.ReadDir(ctx, srcPath, func(chunk []vfs.VFSItem) {
			items = append(items, chunk...)
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Name == ".." {
				continue
			}
			if err := recursiveCopy(ctx, srcVfs, srcVfs.Join(srcPath, item.Name), dstVfs, dstVfs.Join(destPath, item.Name), state, depth+1); err != nil {
				return err
			}
		}
		if state.Tracker != nil {
			state.Tracker.DirDone()
			if state.UpdateUI != nil {
				state.UpdateUI(false)
			}
		}
		return nil
	}

	itemName := dstVfs.Base(destPath)
	if state.Tracker != nil {
		state.Tracker.StartFile(itemName, stat.Size)
		if state.UpdateUI != nil {
			state.UpdateUI(false)
		}
	}

	skipFile := func() {
		state.SkippedCount++
		if state.Tracker != nil {
			state.Tracker.FileSkipped()
			if state.UpdateUI != nil {
				state.UpdateUI(true)
			}
		}
	}

	destPathForFile := destPath

	for {
		dstStat, err := dstVfs.Stat(ctx, destPathForFile)
		exists := err == nil

		if !exists {
			break
		}
		if dstStat.IsDir {
			return fmt.Errorf("cannot overwrite folder with file: %s", dstVfs.Base(destPathForFile))
		}
		if state.SkipAll {
			skipFile()
			return nil
		}
		if state.OverwriteAll {
			break
		}

		choice, remember := AskOverwrite(ctx, destPathForFile, stat, dstStat)
		if choice == 1 { // Overwrite
			if remember {
				state.OverwriteAll = true
				vtui.DebugLog("FILEOP: User chose OVERWRITE ALL")
			}
			break
		} else if choice == 2 { // Skip
			if remember {
				state.SkipAll = true
				vtui.DebugLog("FILEOP: User chose SKIP ALL")
			}
			skipFile()
			return nil
		} else if choice == 3 { // Rename
			newName := AskRename(ctx, dstVfs.Base(destPathForFile))
			if newName == "" {
				return context.Canceled
			}
			destPathForFile = dstVfs.Join(dstVfs.Dir(destPathForFile), newName)
			continue
		} else if choice == 4 || choice == 5 { // Append, Resume
			resultChan := make(chan int, 1)
			vtui.FrameManager.PostTask(func() {
				errDlg := vtui.ShowMessage(" Unsupported ", "Append/Resume not supported by current VFS implementation.", []string{"&Ok"})
				errDlg.OnResult = func(c int) { resultChan <- c }
			})
			<-resultChan
			continue
		} else { // Cancel
			return context.Canceled
		}
	}

	var srcFile vfs.ReadAtCloser
	var dstFile io.WriteCloser

	for {
		srcFile, err = srcVfs.Open(ctx, srcPath)
		if err == nil {
			break
		}
		choice := AskError(ctx, "Cannot open source file", err)
		if choice == 1 {
			skipFile()
			return nil
		}
		if choice == 2 {
			return context.Canceled
		}
	}
	defer srcFile.Close()

	for {
		dstFile, err = dstVfs.Create(ctx, destPathForFile)
		if err == nil {
			break
		}
		choice := AskError(ctx, "Cannot create destination file", err)
		if choice == 1 {
			skipFile()
			return nil
		}
		if choice == 2 {
			return context.Canceled
		}
	}
	defer dstFile.Close()

	buf := make([]byte, 128*1024)
	for {
		if ctx.Err() != nil { return ctx.Err() }
		n, rerr := srcFile.Read(ctx, buf)
		if n > 0 {
			if _, werr := dstFile.Write(buf[:n]); werr != nil {
				return werr
			}
			if state.OnBytes != nil {
				state.OnBytes(n)
			}
		}
		if rerr != nil {
			if rerr == io.EOF { break }
			return rerr
		}
	}

	if state.Tracker != nil {
		state.Tracker.FileDone()
		if state.UpdateUI != nil {
			state.UpdateUI(false)
		}
	}
	return nil
}

// AskOverwrite shows a rich modal dialog for file conflicts.
func AskOverwrite(ctx context.Context, destPath string, srcStat, dstStat vfs.VFSItem) (int, bool) {
	resultChan := make(chan int, 1)
	rememberChan := make(chan bool, 1)
	var dlg *vtui.Window

	vtui.FrameManager.PostTask(func() {
		if ctx.Err() != nil { return }

		width := 76
		height := 13
		dlg = vtui.NewCenteredDialog(width, height, " Warning ")

		lbl1 := vtui.NewLabel(0, 0, "File already exists", nil)
		truncPath := vtui.TruncateMiddle(destPath, width-6)
		lbl2 := vtui.NewLabel(0, 0, truncPath, nil)

		sep1 := vtui.NewSeparator(0, 0, width-4, true, true)

		formatInfo := func(label string, stat vfs.VFSItem) string {
			dateStr := stat.MTime.Format("02.01.2006 15:04:05")
			return fmt.Sprintf("%-10s %15d  %s", label, stat.Size, dateStr)
		}

		lblNew := vtui.NewLabel(0, 0, formatInfo("New", srcStat), nil)
		lblExist := vtui.NewLabel(0, 0, formatInfo("Existing", dstStat), nil)

		sep2 := vtui.NewSeparator(0, 0, width-4, true, true)

		chkRem := vtui.NewCheckbox(0, 0, "Reme&mber choice", false)

		sep3 := vtui.NewSeparator(0, 0, width-4, true, true)

		btnOver := vtui.NewButton(0, 0, "&Overwrite")
		btnOver.IsDefault = true
		btnSkip := vtui.NewButton(0, 0, "&Skip")
		btnRen := vtui.NewButton(0, 0, "&Rename")
		btnApp := vtui.NewButton(0, 0, "&Append")
		btnRes := vtui.NewButton(0, 0, "Res&ume")
		btnCan := vtui.NewButton(0, 0, "&Cancel")

		dlg.AddItem(lbl1)
		dlg.AddItem(lbl2)
		dlg.AddItem(sep1)
		dlg.AddItem(lblNew)
		dlg.AddItem(lblExist)
		dlg.AddItem(sep2)
		dlg.AddItem(chkRem)
		dlg.AddItem(sep3)
		dlg.AddItem(btnOver)
		dlg.AddItem(btnSkip)
		dlg.AddItem(btnRen)
		dlg.AddItem(btnApp)
		dlg.AddItem(btnRes)
		dlg.AddItem(btnCan)

		vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, width-4, height-4)
		vbox.Add(lbl1, vtui.Margins{}, vtui.AlignCenter)
		vbox.Add(lbl2, vtui.Margins{}, vtui.AlignCenter)
		vbox.Add(sep1, vtui.Margins{}, vtui.AlignFill)
		vbox.Add(lblNew, vtui.Margins{}, vtui.AlignLeft)
		vbox.Add(lblExist, vtui.Margins{}, vtui.AlignLeft)
		vbox.Add(sep2, vtui.Margins{}, vtui.AlignFill)
		vbox.Add(chkRem, vtui.Margins{}, vtui.AlignLeft)
		vbox.Add(sep3, vtui.Margins{}, vtui.AlignFill)

		hbox := vtui.NewHBoxLayout(0, 0, width-4, 1)
		hbox.HorizontalAlign = vtui.AlignCenter
		hbox.Spacing = 1
		hbox.Add(btnOver, vtui.Margins{}, vtui.AlignTop)
		hbox.Add(btnSkip, vtui.Margins{}, vtui.AlignTop)
		hbox.Add(btnRen, vtui.Margins{}, vtui.AlignTop)
		hbox.Add(btnApp, vtui.Margins{}, vtui.AlignTop)
		hbox.Add(btnRes, vtui.Margins{}, vtui.AlignTop)
		hbox.Add(btnCan, vtui.Margins{}, vtui.AlignTop)

		vbox.Add(hbox, vtui.Margins{}, vtui.AlignFill)
		vbox.Apply()

		btnOver.OnClick = func() { resultChan <- 1; rememberChan <- (chkRem.State == 1); dlg.Close() }
		btnSkip.OnClick = func() { resultChan <- 2; rememberChan <- (chkRem.State == 1); dlg.Close() }
		btnRen.OnClick = func() { resultChan <- 3; rememberChan <- (chkRem.State == 1); dlg.Close() }
		btnApp.OnClick = func() { resultChan <- 4; rememberChan <- (chkRem.State == 1); dlg.Close() }
		btnRes.OnClick = func() { resultChan <- 5; rememberChan <- (chkRem.State == 1); dlg.Close() }
		btnCan.OnClick = func() { resultChan <- 6; rememberChan <- false; dlg.Close() }

		dlg.OnResult = func(code int) {
			if code < 0 {
				select {
				case resultChan <- 6:
					rememberChan <- false
				default:
				}
			}
		}
		vtui.FrameManager.Push(dlg)
	})

	select {
	case res := <-resultChan:
		rem := <-rememberChan
		return res, rem
	case <-ctx.Done():
		vtui.FrameManager.PostTask(func() {
			if dlg != nil && !dlg.IsDone() { dlg.Close() }
		})
		return 6, false
	}
}

func AskRename(ctx context.Context, oldName string) string {
	resultChan := make(chan string, 1)
	var dlg *vtui.Window
	vtui.FrameManager.PostTask(func() {
		if ctx.Err() != nil { return }
		dlg = vtui.InputBox(" Rename ", "New name:", oldName, func(s string) {
			select { case resultChan <- s: default: }
		})
		dlg.OnResult = func(code int) {
			if code < 0 {
				select { case resultChan <- "": default: }
			}
		}
	})
	select {
	case res := <-resultChan:
		return res
	case <-ctx.Done():
		vtui.FrameManager.PostTask(func() {
			if dlg != nil && !dlg.IsDone() { dlg.Close() }
		})
		return ""
	}
}

// AskError handles I/O errors by asking user for Retry/Skip/Abort
func AskError(ctx context.Context, op string, err error) int {
	resultChan := make(chan int, 1)
	var dlg *vtui.Window

	vtui.FrameManager.PostTask(func() {
		if ctx.Err() != nil { return }
		msg := fmt.Sprintf("%s:\n%s\n\n%s", op, err.Error(), "What to do?")
		dlg = vtui.ShowMessage(" Error ", msg, []string{Msg("Btn.Retry"), "&Skip", "&Abort"})
		dlg.OnResult = func(code int) {
			if code < 0 {
				code = 2
			}
			select { case resultChan <- code: default: }
		}
	})

	select {
	case res := <-resultChan:
		return res
	case <-ctx.Done():
		vtui.FrameManager.PostTask(func() {
			if dlg != nil && !dlg.IsDone() { dlg.Close() }
		})
		return 2 // 2 matches Abort button index
	}
}
