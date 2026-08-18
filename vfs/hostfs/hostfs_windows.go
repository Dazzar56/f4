//go:build windows

// See hostfs_posix.go for the package doc.
//
// This is Stage E1 (WINE.md §13.9): a pure mechanical refactor. Every
// function here forwards to os.* exactly as os_vfs.go did before this
// package existed -- behavior does not change on any platform. The actual
// personality switch (posix via libwinescape vs windows via os.*) lands in
// Stages E2/E3; this file is deliberately the "always windows" half of that
// switch until it exists, so introducing this package carries zero risk on
// its own.
package hostfs

import (
	"os"
	"time"
)

func Open(name string) (*os.File, error) { return os.Open(name) }

func OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func Stat(name string) (os.FileInfo, error)  { return os.Stat(name) }
func Lstat(name string) (os.FileInfo, error) { return os.Lstat(name) }

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
