//go:build windows

// See hostpath_posix.go for the package doc.
//
// Stage E1 (WINE.md §13.9): pure mechanical refactor, forwards to
// path/filepath exactly as os_vfs.go did before this package existed.
// The posix-personality path semantics (plain "/"-based paths, no volume
// names) land in Stage E4; until then this is deliberately the
// "always windows" half of the eventual switch, so introducing this
// package carries zero behavioral risk on its own.
package hostpath

import "path/filepath"

func Join(elem ...string) string               { return filepath.Join(elem...) }
func Dir(path string) string                   { return filepath.Dir(path) }
func Base(path string) string                  { return filepath.Base(path) }
func Clean(path string) string                 { return filepath.Clean(path) }
func IsAbs(path string) bool                   { return filepath.IsAbs(path) }
func VolumeName(path string) string            { return filepath.VolumeName(path) }
func Abs(path string) (string, error)          { return filepath.Abs(path) }
func EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }

const Separator = filepath.Separator
