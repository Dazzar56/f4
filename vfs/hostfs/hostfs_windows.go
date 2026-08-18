//go:build windows

// See hostfs_posix.go for the package doc.
//
// WINE.md Stage E3: every function now genuinely branches on
// vfs/hostmode.Posix(). In posix mode, file operations go through
// libwinescape (hostfs_winescape.go) -- a raw fd, real POSIX errno, no
// Win32 anywhere in the call chain. In windows mode, behavior is
// byte-for-byte what it was before this package existed: plain os.*.
package hostfs

import (
	"io/fs"
	"os"
	"time"

	winescape "github.com/unxed/libwinescape/go"

	"github.com/unxed/f4/vfs/hostmode"
)

// File -- see hostfs_posix.go. Declared identically here since Go doesn't
// let a build-tag-split package share a type declaration across files any
// more cleanly than this without a third, tag-free file; duplicating an
// interface declaration costs nothing and keeps each variant self-contained
// and readable on its own.
type File interface {
	Read(p []byte) (n int, err error)
	ReadAt(p []byte, off int64) (n int, err error)
	Write(p []byte) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
	Stat() (os.FileInfo, error)
	Truncate(size int64) error
	Close() error
}

func Open(name string) (File, error) {
	if hostmode.Posix() {
		return winescapeOpenFile(name, os.O_RDONLY, 0)
	}
	return os.Open(name)
}

func OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	if hostmode.Posix() {
		return winescapeOpenFile(name, flag, perm)
	}
	return os.OpenFile(name, flag, perm)
}

func Stat(name string) (os.FileInfo, error) {
	if hostmode.Posix() {
		var st winescape.Stat_t
		if err := winescape.Stat(name, &st); err != nil {
			return nil, err
		}
		return wineStatToInfo("", name, st), nil
	}
	return os.Stat(name)
}

func Lstat(name string) (os.FileInfo, error) {
	if hostmode.Posix() {
		var st winescape.Stat_t
		if err := winescape.Lstat(name, &st); err != nil {
			return nil, err
		}
		return wineStatToInfo("", name, st), nil
	}
	return os.Lstat(name)
}

func ReadDir(name string) ([]fs.DirEntry, error) {
	if hostmode.Posix() {
		return winescapeReadDir(name)
	}
	return os.ReadDir(name)
}

func Readlink(name string) (string, error) {
	if hostmode.Posix() {
		buf := make([]byte, 4096)
		n, err := winescape.Readlink(name, buf)
		if err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}
	return os.Readlink(name)
}

func Symlink(oldname, newname string) error {
	if hostmode.Posix() {
		return winescape.Symlink(oldname, newname)
	}
	return os.Symlink(oldname, newname)
}

func Link(oldname, newname string) error {
	if hostmode.Posix() {
		// libwinescape does not expose link(2)/linkat(2) yet. Fail plainly
		// rather than silently falling back to a Win32 hardlink across what
		// is, in posix mode, not even a Win32-shaped path.
		return errNotImplemented("Link")
	}
	return os.Link(oldname, newname)
}

func Rename(oldpath, newpath string) error {
	if hostmode.Posix() {
		return winescape.Rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func RemoveAll(path string) error {
	if hostmode.Posix() {
		return winescape.RemoveAll(path)
	}
	return os.RemoveAll(path)
}

func Remove(name string) error {
	if hostmode.Posix() {
		return winescapeRemove(name)
	}
	return os.Remove(name)
}

func MkdirAll(path string, perm os.FileMode) error {
	if hostmode.Posix() {
		return winescape.MkdirAll(path, uint32(perm.Perm()))
	}
	return os.MkdirAll(path, perm)
}

func Mkdir(name string, perm os.FileMode) error {
	if hostmode.Posix() {
		return winescape.Mkdir(name, uint32(perm.Perm()))
	}
	return os.Mkdir(name, perm)
}

func Chmod(name string, mode os.FileMode) error {
	if hostmode.Posix() {
		return winescape.Chmod(name, uint32(mode.Perm()))
	}
	return os.Chmod(name, mode)
}

func Chown(name string, uid, gid int) error {
	if hostmode.Posix() {
		return winescape.Chown(name, uid, gid)
	}
	return os.Chown(name, uid, gid)
}

func Lchown(name string, uid, gid int) error {
	if hostmode.Posix() {
		return winescape.Lchown(name, uid, gid)
	}
	return os.Lchown(name, uid, gid)
}

func Chtimes(name string, atime, mtime time.Time) error {
	if hostmode.Posix() {
		return winescape.Chtimes(name, atime, mtime)
	}
	return os.Chtimes(name, atime, mtime)
}
