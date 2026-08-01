package vfs

import (
	"context"
	"fmt"
)

// OpStats holds detailed statistics about a file system subtree.
// Separating Files/Dirs from Bytes allows for highly accurate,
// non-linear ETA calculations during I/O operations. DirBytes is the
// sum of directory-inode sizes (as reported by Stat) and is tracked
// separately so ETA calculations for copy/move — which only move the
// file payload — stay unaffected; consumers that want the far2l-style
// "Files size" for display (Ctrl+Q quick view) can add Bytes+DirBytes.
type OpStats struct {
	Bytes    int64
	DirBytes int64
	Files    int64
	Dirs     int64
}

// Add merges another OpStats into the current one.
func (s *OpStats) Add(other OpStats) {
	s.Bytes += other.Bytes
	s.DirBytes += other.DirBytes
	s.Files += other.Files
	s.Dirs += other.Dirs
}

// ScanCallback is used to report progress during a long scanning operation.
// It returns the path currently being inspected and the accumulated stats so far.
type ScanCallback func(currentPath string, stats OpStats)

// FastScanner is an optional interface for VFS implementations.
// If a VFS implements this, it means it can offload the tree traversal
// to the remote server (e.g., FISH+), drastically reducing network roundtrips.
type FastScanner interface {
	Scan(ctx context.Context, basePath string, names []string, cb ScanCallback) (OpStats, error)
}

// CalculateStats is the main entry point for gathering operation statistics.
// It uses FastScanner if available, otherwise falls back to GenericScan.
func CalculateStats(ctx context.Context, v VFS, basePath string, names []string, cb ScanCallback) (OpStats, error) {
	if fs, ok := v.(FastScanner); ok {
		return fs.Scan(ctx, basePath, names, cb)
	}
	return GenericScan(ctx, v, basePath, names, cb)
}

// GenericScan performs a recursive, client-side tree traversal to gather stats.
func GenericScan(ctx context.Context, v VFS, basePath string, names []string, cb ScanCallback) (OpStats, error) {
	var totalStats OpStats

	for _, name := range names {
		if ctx.Err() != nil {
			return totalStats, ctx.Err()
		}

		fullPath := v.Join(basePath, name)
		itemStat, err := v.Stat(ctx, fullPath)
		if err != nil {
			// If we can't stat the root item, we abort.
			// (During actual copy, AskError handles this, but for pre-scan we just return the error).
			return totalStats, err
		}

		err = scanRecursive(ctx, v, fullPath, itemStat, &totalStats, cb, 0)
		if err != nil {
			return totalStats, err
		}
	}

	return totalStats, nil
}

func scanRecursive(ctx context.Context, v VFS, currentPath string, item VFSItem, stats *OpStats, cb ScanCallback, depth int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Prevent infinite recursion (e.g., cyclic symlinks not caught by VFS)
	if depth > 1000 {
		return fmt.Errorf("maximum recursion depth exceeded at %s", currentPath)
	}

	if cb != nil {
		// Report progress. To avoid spamming the UI thread, the caller should throttle this.
		cb(currentPath, *stats)
	}

	if !item.IsDir {
		stats.Files++
		stats.Bytes += item.Size
		return nil
	}

	// It's a directory
	stats.Dirs++
	stats.DirBytes += item.Size

	var childItems []VFSItem
	err := v.ReadDir(ctx, currentPath, func(chunk []VFSItem) {
		childItems = append(childItems, chunk...)
	})
	if err != nil {
		// Permission denied on a subfolder shouldn't necessarily fail the whole scan,
		// but returning the error lets the caller decide. For now, we propagate it.
		return err
	}

	for _, child := range childItems {
		if child.Name == ".." {
			continue
		}
		childPath := v.Join(currentPath, child.Name)
		err := scanRecursive(ctx, v, childPath, child, stats, cb, depth+1)
		if err != nil {
			return err
		}
	}

	return nil
}
