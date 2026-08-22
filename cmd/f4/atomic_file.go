package main

import (
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomically publishes a complete file without exposing a truncated
// target. The temporary file lives beside the target so Rename is atomic on
// filesystems that support atomic same-directory replacement.
func writeFileAtomically(path string, data []byte, mode os.FileMode) (returnErr error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, ".f4-atomic-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := f.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if returnErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := f.Chmod(mode); err != nil {
		return err
	}
	for len(data) > 0 {
		written, err := f.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	// The deferred Close is now harmless and the temp name is removed only if
	// publication fails.
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	returnErr = nil
	// Directory fsync is a best-effort durability improvement. It is not
	// available on every supported platform, while the same-directory rename
	// remains the atomic publication point.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
