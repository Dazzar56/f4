package archive

import (
	"context"
	"errors"
	"io"

	"github.com/unxed/archives"
	"github.com/unxed/vtui"
	zipperarchive "github.com/unxed/zipper/archive"
)

type archivePasswordResult struct {
	password string
	err      error
}

// archivePasswordPrompt is replaceable in tests so password handling can be
// verified without depending on an interactive terminal.
var archivePasswordPrompt = promptArchivePassword

func promptArchivePassword(ctx context.Context, archiveName string) (string, error) {
	if vtui.FrameManager == nil {
		return "", errors.New("archive: cannot request a password without an active UI")
	}

	result := make(chan archivePasswordResult, 1)
	vtui.FrameManager.PostTask(func() {
		showArchivePasswordDialog(archiveName, result)
	})

	select {
	case value := <-result:
		return value.password, value.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func showArchivePasswordDialog(archiveName string, result chan<- archivePasswordResult) {
	dlg := vtui.NewCenteredDialog(52, 7, " Archive password ")
	dlg.ShowClose = true

	x := dlg.X1 + 2
	y := dlg.Y1 + 2
	password := vtui.NewPasswordEdit(x+12, y, 34, "")
	dlg.AddItem(vtui.NewLabel(x, y, "Password:", password))
	dlg.AddItem(password)

	ok := vtui.NewButton(dlg.X1+15, dlg.Y2-2, "&OK")
	ok.IsDefault = true
	cancel := vtui.NewButton(dlg.X1+28, dlg.Y2-2, "&Cancel")
	dlg.AddItem(ok)
	dlg.AddItem(cancel)

	finished := false
	finish := func(value archivePasswordResult) {
		if finished {
			return
		}
		finished = true
		result <- value
	}
	ok.OnClick = func() {
		finish(archivePasswordResult{password: password.GetText()})
		password.SetText("")
		dlg.Close()
	}
	cancel.OnClick = func() { dlg.Close() }
	dlg.OnResult = func(code int) {
		if code < 0 {
			finish(archivePasswordResult{err: context.Canceled})
		}
	}

	vtui.FrameManager.Push(dlg)
}

func openArchiveFSWithPasswordPrompt(ctx context.Context, localPath, displayName string, backing io.Closer) (zipperarchive.FileSystem, string, bool, error) {
	var password string
	for {
		fsys, cleanupTransferred, err := openArchiveFSWithContext(ctx, localPath, displayName, backing, password)
		if err == nil {
			return fsys, password, cleanupTransferred, nil
		}
		if !zipperarchive.IsPasswordError(err) {
			return nil, "", cleanupTransferred, err
		}

		password, err = archivePasswordPrompt(ctx, displayName)
		if err != nil {
			return nil, "", cleanupTransferred, err
		}
		if password == "" {
			return nil, "", cleanupTransferred, errors.New("archive password was not provided")
		}
	}
}

func (v *ArchiveVFS) openWithPassword(ctx context.Context, cause error) error {
	if !zipperarchive.IsPasswordError(cause) {
		return cause
	}

	password, err := archivePasswordPrompt(ctx, v.displayName)
	if err != nil {
		return err
	}
	if password == "" {
		return cause
	}

	v.mu.Lock()
	localPath := v.activePath()
	displayName := v.displayName
	v.mu.Unlock()
	fsys, _, err := openArchiveFSWithContext(ctx, localPath, displayName, nil, password)
	if err != nil {
		return err
	}

	v.mu.Lock()
	if v.isClosed {
		v.mu.Unlock()
		_ = fsys.Close()
		return errors.New("archive VFS is closed")
	}
	v.cancelCleanupLocked()
	oldFS := v.fsys
	v.fsys = fsys
	v.password = password
	v.mu.Unlock()
	if oldFS != nil {
		_ = oldFS.Close()
	}
	return nil
}

func archivePasswordFormat(format archives.Format, password string) (archives.Format, bool) {
	if password == "" {
		return format, false
	}
	switch format := format.(type) {
	case archives.Rar:
		format.Password = password
		return format, true
	case *archives.Rar:
		format.Password = password
		return format, true
	case archives.SevenZip:
		format.Password = password
		return format, true
	case *archives.SevenZip:
		format.Password = password
		return format, true
	default:
		return format, false
	}
}
