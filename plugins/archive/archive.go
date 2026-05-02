package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zip"
	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
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
	if srcVfs == nil || dstVfs == nil { return }

	name := app.GetSelectedName()
	if name == "" || name == ".." { return }

	srcPath := srcVfs.Join(srcVfs.GetPath(), name)
	destDir := dstVfs.GetPath()

	app.RunProgressTask(" Extracting... ", "Identifying archive...", false, func(ctx context.Context, update func(msg string, percent int)) error {
		f, err := os.Open(srcPath)
		if err != nil { return err }
		defer f.Close()

		format, _, err := archives.Identify(ctx, srcPath, f)
		if err != nil { return err }

		ex, ok := format.(archives.Extractor)
		if !ok { return fmt.Errorf("file is not an extractable archive") }

		f.Seek(0, io.SeekStart)

		type extractState struct {
			OverwriteAll bool
			SkipAll      bool
		}
		state := &extractState{}

		return ex.Extract(ctx, f, func(ctx context.Context, info archives.FileInfo) error {
			if ctx.Err() != nil { return ctx.Err() }
			update(fmt.Sprintf("Extracting: %s", info.NameInArchive), -1)
			targetPath := filepath.Join(destDir, info.NameInArchive)

			if info.IsDir() {
				return os.MkdirAll(targetPath, 0755)
			}

			// Check for conflict before creating the file
			if _, err := os.Stat(targetPath); err == nil {
				if state.SkipAll {
					return nil // Silently skip
				}
				if !state.OverwriteAll {
					msg := fmt.Sprintf("File already exists:\n%s\n\nOverwrite?", info.NameInArchive)
					buttons := []string{"&Overwrite", "Overwrite &All", "&Skip", "S&kip All", "&Cancel"}
					choice := app.Message(" Conflict ", msg, buttons)

					switch choice {
					case 0: // Overwrite
						// Proceed
					case 1: // Overwrite All
						state.OverwriteAll = true
					case 2: // Skip
						return nil
					case 3: // Skip All
						state.SkipAll = true
						return nil
					default: // Cancel or closed dialog
						return context.Canceled
					}
				}
			}

			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			out, err := os.Create(targetPath)
			if err != nil { return err }
			defer out.Close()

			in, err := info.Open()
			if err != nil { return err }
			defer in.Close()

			_, err = io.Copy(out, &ioCtxReader{r: in, ctx: ctx})
			return err
		})
	}, func(err error) {
		if err != nil && err != context.Canceled {
			app.Message(" Error ", fmt.Sprintf("Extraction failed:\n%v", err), []string{"&Ok"})
		}
		app.RefreshAll()
	})
}

func actionAddArchive(app vfs.App) {
	activeVfs := app.GetActivePanelVFS()
	if activeVfs == nil { return }

	names := app.GetSelectedNames()
	if len(names) == 0 { return }

	arcName := activeVfs.Base(activeVfs.GetPath())
	if arcName == "." || arcName == "" { arcName = "archive" }
	arcName += ".zip"

	app.InputBox(" Add to archive ", "Archive name:", arcName, func(name string) {
		if name == "" { return }
		fullArcPath := activeVfs.Join(activeVfs.GetPath(), name)

		app.RunProgressTask(" Archiving... ", "Gathering files...", false, func(ctx context.Context, update func(msg string, percent int)) error {
			var files []archives.FileInfo
			for i, n := range names {
				if ctx.Err() != nil { return ctx.Err() }
				update(fmt.Sprintf("Scanning: %s", n), (i*100)/len(names))
				fullPath := activeVfs.Join(activeVfs.GetPath(), n)
				if osvfs, ok := activeVfs.(*vfs.OSVFS); ok {
					absPath, _ := osvfs.Abs(fullPath)
					moreFiles, err := archives.FilesFromDisk(ctx, nil, map[string]string{absPath: n})
					if err == nil { files = append(files, moreFiles...) }
				}
			}
			out, err := os.Create(fullArcPath)
			if err != nil { return err }
			defer out.Close()
			return archives.Zip{
				Compression: zip.Deflate,
			}.Archive(ctx, out, files)
		}, func(err error) {
			if err != nil && err != context.Canceled {
				app.Message(" Error ", fmt.Sprintf("Archiving failed:\n%v", err), []string{"&Ok"})
			}
			app.RefreshAll()
		})
	})
}

func (p *ArchivePlugin) Close() error { return nil }
func (p *ArchivePlugin) GetName() string { return "Archive Support" }