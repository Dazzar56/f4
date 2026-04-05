package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

type ArchivePlugin struct{}

func (p *ArchivePlugin) Init(api vfs.HostAPI) error {
	api.RegisterVFSProvider(&vfs.ArchiveProvider{})

	api.RegisterGlobalHotkey(vtinput.VK_F1, vtinput.ShiftPressed, func(app vfs.App) {
		actionArchiveCommands(app)
	})

	return nil
}

func actionArchiveCommands(app vfs.App) {
	menu := vtui.NewVMenu(" Archive Commands ")
	menu.AddItem(vtui.MenuItem{Text: "&1. Add to archive"})
	menu.AddItem(vtui.MenuItem{Text: "&2. Extract files"})

	menu.OnAction = func(idx int) {
		menu.Close()
		switch idx {
		case 0:
			actionAddArchive(app)
		case 1:
			actionExtractArchive(app)
		}
	}

	vtui.FrameManager.Push(menu)
}

func actionExtractArchive(app vfs.App) {
	srcVfs := app.GetActivePanelVFS()
	dstVfs := app.GetPassivePanelVFS()
	if srcVfs == nil || dstVfs == nil { return }

	name := app.GetSelectedName()
	if name == "" || name == ".." { return }

	srcPath := srcVfs.Join(srcVfs.GetPath(), name)
	destDir := dstVfs.GetPath()

	app.RunProgressTask(" Extracting... ", "Identifying archive...", false, func(tctx *vtui.TaskContext, update func(msg string, percent int)) error {
		f, err := os.Open(srcPath)
		if err != nil { return err }
		defer f.Close()

		format, _, err := archives.Identify(tctx.Context, srcPath, f)
		if err != nil { return err }

		ex, ok := format.(archives.Extractor)
		if !ok { return fmt.Errorf("file is not an extractable archive") }

		f.Seek(0, io.SeekStart)

		return ex.Extract(tctx.Context, f, func(ctx context.Context, info archives.FileInfo) error {
			if tctx.Err() != nil { return tctx.Err() }
			update(fmt.Sprintf("Extracting: %s", info.NameInArchive), -1)
			targetPath := filepath.Join(destDir, info.NameInArchive)
			if info.IsDir() { return os.MkdirAll(targetPath, 0755) }
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

	vtui.InputBox(" Add to archive ", "Archive name:", arcName, func(name string) {
		if name == "" { return }
		fullArcPath := activeVfs.Join(activeVfs.GetPath(), name)

		app.RunProgressTask(" Archiving... ", "Gathering files...", false, func(tctx *vtui.TaskContext, update func(msg string, percent int)) error {
			var files []archives.FileInfo
			for i, n := range names {
				if tctx.Err() != nil { return tctx.Err() }
				update(fmt.Sprintf("Scanning: %s", n), (i*100)/len(names))
				fullPath := activeVfs.Join(activeVfs.GetPath(), n)
				if osvfs, ok := activeVfs.(*vfs.OSVFS); ok {
					absPath, _ := osvfs.Abs(fullPath)
					moreFiles, err := archives.FilesFromDisk(tctx.Context, nil, map[string]string{absPath: n})
					if err == nil { files = append(files, moreFiles...) }
				}
			}
			out, err := os.Create(fullArcPath)
			if err != nil { return err }
			defer out.Close()
			return archives.Zip{}.Archive(tctx.Context, out, files)
		}, func(err error) {
			if err != nil && err != context.Canceled {
				vtui.ShowMessage(" Error ", fmt.Sprintf("Archiving failed:\n%v", err), []string{"&Ok"})
			}
			app.RefreshAll()
		})
	})
}

func (p *ArchivePlugin) Close() error { return nil }
func (p *ArchivePlugin) GetName() string { return "Archive Support" }