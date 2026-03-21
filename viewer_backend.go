package main

import (
	"io"
	"os"
)

// ViewerBackend provides efficient random access to a potentially huge file.
type ViewerBackend struct {
	file *os.File
	size int64
}

func NewViewerBackend(path string) (*ViewerBackend, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &ViewerBackend{
		file: f,
		size: info.Size(),
	}, nil
}

func (b *ViewerBackend) Close() error {
	return b.file.Close()
}

func (b *ViewerBackend) Size() int64 {
	return b.size
}

// ReadAt reads length bytes starting from offset.
func (b *ViewerBackend) ReadAt(offset int64, length int) ([]byte, error) {
	if offset >= b.size {
		return nil, io.EOF
	}
	if offset+int64(length) > b.size {
		length = int(b.size - offset)
	}
	buf := make([]byte, length)
	_, err := b.file.ReadAt(buf, offset)
	return buf, err
}

// FindLineStart looks backwards from the given offset to find the start of the current line.
func (b *ViewerBackend) FindLineStart(offset int64) int64 {
	if offset <= 0 {
		return 0
	}
	chunkSize := int64(4096)
	curr := offset
	for curr > 0 {
		start := curr - chunkSize
		if start < 0 {
			start = 0
		}
		data, err := b.ReadAt(start, int(curr-start))
		if err != nil {
			return 0
		}
		for i := len(data) - 1; i >= 0; i-- {
			if data[i] == '\n' {
				return start + int64(i) + 1
			}
		}
		curr = start
	}
	return 0
}