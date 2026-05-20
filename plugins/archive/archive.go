package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/tar"
	"github.com/unxed/vtinput"
	"github.com/unxed/zip"
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

	app.RunProgressTask(" Extracting... ", "Identifying archive...", false, func(ctx context.Context, update func(msg string, percent int)) error {
		f, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer f.Close()

		format, _, err := archives.Identify(ctx, srcPath, f)
		if err != nil {
			return err
		}

		isTar := false
		if _, ok := format.(archives.Tar); ok {
			isTar = true
		} else if ca, ok := format.(archives.CompressedArchive); ok {
			if _, ok := ca.Archival.(archives.Tar); ok {
				isTar = true
			}
		}

		if isTar {
			f.Close()
			update("Extracting tar archive...", -1)
			e, err := tar.NewExtractor(srcPath, destDir)
			if err != nil {
				return err
			}
			defer e.Close()
			return e.Extract(ctx)
		}

		ex, ok := format.(archives.Extractor)
		if !ok {
			return fmt.Errorf("file is not an extractable archive")
		}

		f.Seek(0, io.SeekStart)

		type extractState struct {
			OverwriteAll bool
			SkipAll      bool
		}
		state := &extractState{}

		return ex.Extract(ctx, f, func(ctx context.Context, info archives.FileInfo) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
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
			if err != nil {
				return err
			}
			defer out.Close()

			in, err := info.Open()
			if err != nil {
				return err
			}
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

			lowerName := strings.ToLower(name)
			isTar := strings.HasSuffix(lowerName, ".tar") || strings.Contains(lowerName, ".tar.") || strings.HasSuffix(lowerName, ".tgz") || strings.HasSuffix(lowerName, ".txz")

			if isTar {
				method := tar.Store
				if strings.HasSuffix(lowerName, ".gz") || strings.HasSuffix(lowerName, ".tgz") {
					method = tar.GZIP
				} else if strings.HasSuffix(lowerName, ".bz2") {
					method = tar.BZIP2
				} else if strings.HasSuffix(lowerName, ".xz") || strings.HasSuffix(lowerName, ".txz") {
					method = tar.XZ
				} else if strings.HasSuffix(lowerName, ".zst") {
					method = tar.ZSTD
				}

				idxPath, _ := tar.GetStandardIndexPath(fullArcPath)
				archiver, err := tar.NewArchiver(fullArcPath, activeVfs.GetPath(), tar.WithArchiverMethod(method), tar.WithArchiverIndex(idxPath))
				if err != nil {
					return err
				}
				defer archiver.Close()
				return archiver.Archive(ctx, fileMap)
			}

			out, err := os.Create(fullArcPath)
			if err != nil {
				return err
			}
			defer out.Close()

			archiver, err := zip.NewArchiver(out, activeVfs.GetPath(), zip.WithArchiverConcurrency(runtime.NumGoroutine()))
			if err != nil {
				return err
			}
			defer archiver.Close()

			return archiver.Archive(ctx, fileMap)
		}, func(err error) {
			if err != nil && err != context.Canceled {
				app.Message(" Error ", fmt.Sprintf("Archiving failed:\n%v", err), []string{"&Ok"})
			}
			app.RefreshAll()
		})
	})
}

func (p *ArchivePlugin) Close() error    { return nil }
func (p *ArchivePlugin) GetName() string { return "Archive Support" }
