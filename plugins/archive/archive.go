package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"sync"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/zipper/archive"
)

var activeOps sync.Map

type ArchivePlugin struct{}

func (p *ArchivePlugin) Init(api vfs.HostAPI) error {
	api.RegisterVFSProvider(&ArchiveProvider{})

	api.RegisterGlobalHotkey(vtinput.VK_F1, vtinput.ShiftPressed, func(app vfs.App) {
		actionArchiveCommands(app)
	})

	return nil
}

type ioCtxReader struct {
	r   io.Reader
	ctx context.Context
}

func (cr *ioCtxReader) Read(p []byte) (int, error) {
	if cr.ctx.Err() != nil {
		return 0, cr.ctx.Err()
	}
	return cr.r.Read(p)
}
func actionArchiveCommands(app vfs.App) {
	app.Menu(" Archive Commands ", []string{"&1. Add to archive", "&2. Extract files"}, func(idx int) {
		switch idx {
		case 0:
			actionAddArchive(app)
		case 1:
			actionExtractArchive(app)
		}
	})
}

func actionExtractArchive(app vfs.App) {
	srcVfs := app.GetActivePanelVFS()
	dstVfs := app.GetPassivePanelVFS()
	if srcVfs == nil || dstVfs == nil {
		return
	}

	name := app.GetSelectedName()
	if name == "" || name == ".." {
		return
	}

	srcPath := srcVfs.Join(srcVfs.GetPath(), name)
	destDir := dstVfs.GetPath()

	if osvfs, ok := srcVfs.(*vfs.OSVFS); ok {
		srcPath, _ = osvfs.Abs(srcPath)
	} else {
		app.Message(" Error ", "Extraction supported only from local filesystem", []string{"&Ok"})
		return
	}

	isBusy := false
	if _, active := activeOps.Load(srcPath); active {
		isBusy = true
	} else if !vfs.GlobalArchiveLockManager.TryLock(srcPath) {
		isBusy = true
	} else {
		// TryLock succeeded, meaning it was NOT busy. We must unlock it here
		// so that the background worker can safely Lock() it later.
		vfs.GlobalArchiveLockManager.Unlock(srcPath)
	}

	waitLock := true
	if isBusy {
		res := app.Message(" Archive Busy ", "This archive is currently being processed.\nRunning multiple operations simultaneously may severely degrade performance.", []string{"&Queue", "&Parallel", "&Cancel"})
		if res == 2 || res < 0 {
			return
		}
		waitLock = (res == 0)
	}

	app.RunProgressTask(" Extracting... ", "Preparing to extract...", false, func(ctx context.Context, update func(msg string, percent int)) error {
		if waitLock {
			update("Waiting in queue...", -1)
			vfs.GlobalArchiveLockManager.Lock(srcPath)
			defer vfs.GlobalArchiveLockManager.Unlock(srcPath)
		}

		ex, err := archive.NewExtractor(srcPath, destDir, archive.Options{Xattrs: false, SafeWrites: true})
		if err != nil {
			return err
		}
		defer ex.Close()
		return ex.Extract(ctx)

	}, func(err error) {
		if err != nil && err != context.Canceled {
			go app.Message(" Error ", fmt.Sprintf("Extraction failed:\n%v", err), []string{"&Ok"})
		}
		app.RefreshAll()
	})
}

func actionAddArchive(app vfs.App) {
	activeVfs := app.GetActivePanelVFS()
	if activeVfs == nil {
		return
	}

	names := app.GetSelectedNames()
	if len(names) == 0 {
		return
	}

	arcName := activeVfs.Base(activeVfs.GetPath())
	if arcName == "." || arcName == "" {
		arcName = "archive"
	}
	arcName += ".zip"

	app.InputBox(" Add to archive ", "Archive name:", arcName, func(name string) {
		if name == "" {
			return
		}
		fullArcPath := activeVfs.Join(activeVfs.GetPath(), name)

		go func() {
			var absArcPath string
			if osvfs, ok := activeVfs.(*vfs.OSVFS); ok {
				absArcPath, _ = osvfs.Abs(fullArcPath)
			} else {
				absArcPath = fullArcPath
			}

			isBusy := false
			if _, active := activeOps.Load(absArcPath); active {
				isBusy = true
			} else if !vfs.GlobalArchiveLockManager.TryLock(absArcPath) {
				isBusy = true
			} else {
				vfs.GlobalArchiveLockManager.Unlock(absArcPath)
			}

			waitLock := true
			if isBusy {
				res := app.Message(" Archive Busy ", "This archive is currently being processed.\nRunning multiple operations simultaneously may severely degrade performance.", []string{"&Queue", "&Parallel", "&Cancel"})
				if res == 2 || res < 0 {
					return
				}
				waitLock = (res == 0)
			}

			if _, err := activeVfs.Stat(context.Background(), fullArcPath); err == nil {
				msg := "The target archive already exists.\nDo you want to overwrite it?"
				if app.Message(" Warning ", msg, []string{"&Yes", "&No"}) != 0 {
					return
				}
			}

			app.RunProgressTask(" Archiving... ", "Gathering files...", false, func(ctx context.Context, update func(msg string, percent int)) error {
				if waitLock {
					update("Waiting in queue...", -1)
					vfs.GlobalArchiveLockManager.Lock(absArcPath)
					defer vfs.GlobalArchiveLockManager.Unlock(absArcPath)
				}
				fileMap := make(map[string]os.FileInfo)
				for i, n := range names {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					update(fmt.Sprintf("Scanning: %s", n), (i*100)/len(names))

					fullPath := activeVfs.Join(activeVfs.GetPath(), n)
					if osvfs, ok := activeVfs.(*vfs.OSVFS); ok {
						absPath, _ := osvfs.Abs(fullPath)
						filepath.Walk(absPath, func(p string, fi os.FileInfo, e error) error {
							if e == nil {
								fileMap[p] = fi
							}
							return nil
						})
					}
				}

				a, err := archive.NewArchiver(fullArcPath, activeVfs.GetPath(), archive.Options{Xattrs: false})
				if err != nil {
					return err
				}
				defer a.Close()
				return a.Archive(ctx, fileMap)
			}, func(err error) {
				if err != nil && err != context.Canceled {
					go app.Message(" Error ", fmt.Sprintf("Archiving failed:\n%v", err), []string{"&Ok"})
				}
				app.RefreshAll()
			})
		}()
	})
}

func (p *ArchivePlugin) Close() error    { return nil }
func (p *ArchivePlugin) GetName() string { return "Archive Support" }
