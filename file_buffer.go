package main

import "github.com/unxed/f4/vfs"

type FileBuffer struct {
	file vfs.ReadAtCloser
	size int
}

func NewFileBuffer(f vfs.ReadAtCloser) *FileBuffer {
	return &FileBuffer{file: f, size: int(f.Size())}
}

func (fb *FileBuffer) Size() int {
	return fb.size
}

func (fb *FileBuffer) Read(offset, length int) []byte {
	if offset < 0 || offset >= fb.size || length <= 0 {
		return nil
	}
	if offset+length > fb.size {
		length = fb.size - offset
	}
	buf := make([]byte, length)
	n, _ := fb.file.ReadAt(buf, int64(offset))
	return buf[:n]
}