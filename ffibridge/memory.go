package ffibridge

import (
	"errors"
	"fmt"
	"unsafe"
)

// maxCStringLen bounds GoStringAt so a missing terminator cannot walk forever.
const maxCStringLen = 1 << 20

// Memory handed out by Alloc is a Go byte slice kept alive in the bridge, and
// its address is handed to the sandbox directly. This is the same arrangement
// purego itself uses when passing Go buffers to C, and it relies on the Go
// garbage collector not moving heap objects.

func (b *Bridge) block(addr uintptr) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	buf, ok := b.blocks[addr]
	if !ok {
		return nil, fmt.Errorf("ffibridge: %#x is not a block owned by this bridge", addr)
	}
	return buf, nil
}

// Alloc reserves a zeroed block and returns its address.
func (b *Bridge) Alloc(size int) (uintptr, error) {
	if size <= 0 {
		return 0, fmt.Errorf("ffibridge: allocation size must be positive, got %d", size)
	}
	if err := b.allow(OpAlloc, fmt.Sprintf("%d", size)); err != nil {
		return 0, err
	}

	max := b.opts.MaxAlloc
	if max <= 0 {
		max = DefaultMaxAlloc
	}

	buf := make([]byte, size)
	addr := uintptr(unsafe.Pointer(&buf[0]))

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, ErrClosed
	}
	if b.allocated+int64(size) > max {
		return 0, fmt.Errorf("ffibridge: allocation of %d bytes exceeds the %d byte budget", size, max)
	}
	b.blocks[addr] = buf
	b.allocated += int64(size)
	return addr, nil
}

// Free releases a block previously returned by Alloc or CString.
func (b *Bridge) Free(addr uintptr) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	buf, ok := b.blocks[addr]
	if !ok {
		return fmt.Errorf("ffibridge: %#x is not a block owned by this bridge", addr)
	}
	delete(b.blocks, addr)
	b.allocated -= int64(len(buf))
	return nil
}

// Bytes exposes a bridge-owned block for direct host-side access. Writes
// through the returned slice are visible to native code.
func (b *Bridge) Bytes(addr uintptr) ([]byte, error) {
	return b.block(addr)
}

// Write copies data into a bridge-owned block at the given offset.
func (b *Bridge) Write(addr uintptr, off int, data []byte) error {
	buf, err := b.block(addr)
	if err != nil {
		return err
	}
	if off < 0 || off+len(data) > len(buf) {
		return fmt.Errorf("ffibridge: write of %d bytes at offset %d is out of bounds for a %d byte block", len(data), off, len(buf))
	}
	copy(buf[off:], data)
	return nil
}

// Read copies bytes out of a bridge-owned block.
func (b *Bridge) Read(addr uintptr, off, n int) ([]byte, error) {
	buf, err := b.block(addr)
	if err != nil {
		return nil, err
	}
	if off < 0 || n < 0 || off+n > len(buf) {
		return nil, fmt.Errorf("ffibridge: read of %d bytes at offset %d is out of bounds for a %d byte block", n, off, len(buf))
	}
	out := make([]byte, n)
	copy(out, buf[off:off+n])
	return out, nil
}

// CString allocates a NUL terminated copy of s and returns its address.
func (b *Bridge) CString(s string) (uintptr, error) {
	addr, err := b.Alloc(len(s) + 1)
	if err != nil {
		return 0, err
	}
	if err := b.Write(addr, 0, []byte(s)); err != nil {
		_ = b.Free(addr)
		return 0, err
	}
	return addr, nil
}

// Peek reads raw memory at an arbitrary address. It is unchecked by nature:
// a bad address crashes the process, exactly as it would in C.
func (b *Bridge) Peek(addr uintptr, n int) ([]byte, error) {
	if err := b.allow(OpPeek, fmt.Sprintf("%#x+%d", addr, n)); err != nil {
		return nil, err
	}
	if addr == 0 {
		return nil, errors.New("ffibridge: peek at a null address")
	}
	if n < 0 {
		return nil, fmt.Errorf("ffibridge: negative peek length %d", n)
	}
	out := make([]byte, n)
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(addr)), n))
	return out, nil
}

// Poke writes raw memory at an arbitrary address.
func (b *Bridge) Poke(addr uintptr, data []byte) error {
	if err := b.allow(OpPoke, fmt.Sprintf("%#x+%d", addr, len(data))); err != nil {
		return err
	}
	if addr == 0 {
		return errors.New("ffibridge: poke at a null address")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(addr)), len(data)), data)
	return nil
}

// GoStringAt reads a NUL terminated string from an arbitrary address.
func (b *Bridge) GoStringAt(addr uintptr) (string, error) {
	if err := b.allow(OpPeek, fmt.Sprintf("%#x", addr)); err != nil {
		return "", err
	}
	if addr == 0 {
		return "", nil
	}
	base := unsafe.Pointer(addr)
	length := 0
	for length < maxCStringLen {
		if *(*byte)(unsafe.Add(base, length)) == 0 {
			break
		}
		length++
	}
	if length >= maxCStringLen {
		return "", fmt.Errorf("ffibridge: no NUL terminator within %d bytes at %#x", maxCStringLen, addr)
	}
	return string(unsafe.Slice((*byte)(base), length)), nil
}
