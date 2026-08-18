//go:build windows

// See hostfs_posix.go for the package doc.
//
// This is still Stage E1 shape (WINE.md §13.9): every function forwards to
// os.* exactly as os_vfs.go did before this package existed -- behavior
// does not change on any platform yet. The actual personality switch
// (posix via libwinescape vs windows via os.*) is Stage E3, landing in a
// separate commit once the libwinescape-backed implementation of the File
// interface below exists and has been verified under live Wine.
package hostfs

import (
	"io/fs"
	"os"
	"time"
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

func Open(name string) (File, error) { return os.Open(name) }

func OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func Stat(name string) (os.FileInfo, error)  { return os.Stat(name) }
func Lstat(name string) (os.FileInfo, error) { return os.Lstat(name) }

func ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

func Readlink(name string) (string, error)  { return os.Readlink(name) }
func Symlink(oldname, newname string) error { return os.Symlink(oldname, newname) }
func Link(oldname, newname string) error    { return os.Link(oldname, newname) }

func Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }
func RemoveAll(path string) error                  { return os.RemoveAll(path) }
func Remove(name string) error                     { return os.Remove(name) }
func MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func Mkdir(name string, perm os.FileMode) error    { return os.Mkdir(name, perm) }

func Chmod(name string, mode os.FileMode) error { return os.Chmod(name, mode) }
func Chown(name string, uid, gid int) error     { return os.Chown(name, uid, gid) }
func Lchown(name string, uid, gid int) error    { return os.Lchown(name, uid, gid) }
func Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(name, atime, mtime)
}
