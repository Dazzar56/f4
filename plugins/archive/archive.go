package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/zipper/archive"
)

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

	app.RunProgressTask(" Extracting... ", "Extracting archive...", false, func(ctx context.Context, update func(msg string, percent int)) error {
		if osvfs, ok := srcVfs.(*vfs.OSVFS); ok {
			srcPath, _ = osvfs.Abs(srcPath)
		} else {
			return fmt.Errorf("extraction supported only from local filesystem")
		}

		ex, err := archive.NewExtractor(srcPath, destDir, archive.Options{Xattrs: true, SafeWrites: true})
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
			if _, err := activeVfs.Stat(context.Background(), fullArcPath); err == nil {
				msg := "The target archive already exists.\nDo you want to overwrite it?"
				if app.Message(" Warning ", msg, []string{"&Yes", "&No"}) != 0 {
					return
				}
			}

			app.RunProgressTask(" Archiving... ", "Gathering files...", false, func(ctx context.Context, update func(msg string, percent int)) error {
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

				a, err := archive.NewArchiver(fullArcPath, activeVfs.GetPath(), archive.Options{Xattrs: true})
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
