//go:build !windows

// Package hostfs is the single point of contact between f4 and the host
// filesystem's raw operations (WINE.md §13, Part E). Every call site that
// needs to open, stat, rename, link, or otherwise touch a file goes through
// here instead of calling package os directly.
//
// On every non-Windows GOOS this file is the whole story: a direct,
// inlinable forward to package os, identical in every observable way to
// calling os.* directly. There is no Wine here and no trace of it in the
// compiled binary -- the personality switch (hostfs_windows.go) exists on
// exactly one GOOS.
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
func MkdirAll(path string, perm os.FileMode) error  { return os.MkdirAll(path, perm) }
func Mkdir(name string, perm os.FileMode) error     { return os.Mkdir(name, perm) }

func Chmod(name string, mode os.FileMode) error       { return os.Chmod(name, mode) }
func Chown(name string, uid, gid int) error           { return os.Chown(name, uid, gid) }
func Lchown(name string, uid, gid int) error          { return os.Lchown(name, uid, gid) }
func Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(name, atime, mtime)
}
