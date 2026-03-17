package main

import "time"

// VFSItem represents a generic file or directory entry in any VFS.
type VFSItem struct {
	Name  string
	Size  int64
	IsDir bool
	MTime time.Time
	Mode  string // Permissions or attributes string
}

// VFS (Virtual File System) is the interface for any data provider.
type VFS interface {
	GetPath() string
	SetPath(path string) error

	ReadDir(path string) ([]VFSItem, error)
	Stat(path string) (VFSItem, error)

	Join(elem ...string) string
	Abs(path string) (string, error)
	Base(path string) string
	Dir(path string) string

	// Future operations
	// Copy(src, dst string) error
	// Move(src, dst string) error
	// Remove(path string) error
	// Mkdir(path string) error
}