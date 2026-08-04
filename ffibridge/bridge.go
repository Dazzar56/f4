// Package ffibridge is the host side of f4's foreign function interface.
//
// It turns a textual C prototype plus a list of plain values into a real
// native call, so that a sandboxed plugin can reach host APIs without f4
// shipping a hand-written wrapper for every function in every system library.
// Everything crossing the sandbox boundary is an integer, a float or a string:
// library handles, function pointers, memory blocks and callbacks are all
// plain addresses, which maps equally well onto Lua and onto wasm imports.
//
// The bridge is built on pureffi (a purego-compatible, cgo-free FFI). Because
// purego's RegisterFunc is fully reflective, the Go function type matching a
// prototype is constructed at run time, which is what makes dynamic calls with
// floats, structs and C-variadic arguments possible at all.
//
// Security: FFI inside a sandbox is, by construction, an escape hatch from
// that sandbox. Options.Allow is the single choke point where the permission
// model will be enforced; until it is wired up, leaving it nil allows
// everything, which is appropriate only for local development.
package ffibridge

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// DefaultMaxAlloc caps bridge-owned memory when Options.MaxAlloc is zero.
const DefaultMaxAlloc = 64 << 20

// Op names an operation the bridge can be asked to perform. It is the unit of
// granularity the permission model works with.
type Op string

const (
	OpOpen     Op = "open"
	OpSym      Op = "sym"
	OpCall     Op = "call"
	OpAlloc    Op = "alloc"
	OpPeek     Op = "peek"
	OpPoke     Op = "poke"
	OpCallback Op = "callback"
)

var (
	// ErrUnsupported is returned when the platform or the build has no FFI.
	ErrUnsupported = errors.New("ffibridge: FFI is not available in this build")
	// ErrClosed is returned once the owning plugin has been torn down.
	ErrClosed = errors.New("ffibridge: bridge is closed")
)

// Options configures one bridge instance.
type Options struct {
	// Allow, when not nil, is consulted before every operation. A non-nil
	// error aborts the operation and is returned to the caller verbatim.
	Allow func(op Op, detail string) error

	// MaxAlloc caps the total size of live blocks allocated through the
	// bridge. Zero means DefaultMaxAlloc.
	MaxAlloc int64
}

type callKey struct {
	fn  uintptr
	sig string
}

// Bridge is one sandbox's view of the host FFI. Each plugin gets its own, so
// that tearing the plugin down releases its libraries and memory. A Bridge is
// safe for concurrent use.
type Bridge struct {
	opts Options

	mu        sync.Mutex
	closed    bool
	libs      map[uintptr]string
	blocks    map[uintptr][]byte
	callbacks map[uintptr]any
	callables map[callKey]reflect.Value
	allocated int64
}

// New creates an empty bridge.
func New(opts Options) *Bridge {
	return &Bridge{
		opts:      opts,
		libs:      make(map[uintptr]string),
		blocks:    make(map[uintptr][]byte),
		callbacks: make(map[uintptr]any),
		callables: make(map[callKey]reflect.Value),
	}
}

func (b *Bridge) allow(op Op, detail string) error {
	if b.opts.Allow == nil {
		return nil
	}
	return b.opts.Allow(op, detail)
}

func (b *Bridge) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// Open loads a shared library and returns its handle.
func (b *Bridge) Open(name string) (uintptr, error) {
	if err := b.allow(OpOpen, name); err != nil {
		return 0, err
	}
	if !Supported {
		return 0, ErrUnsupported
	}
	if b.isClosed() {
		return 0, ErrClosed
	}

	handle, err := dlOpen(name)
	if err != nil {
		return 0, fmt.Errorf("ffibridge: cannot open %q: %w", name, err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = dlClose(handle)
		return 0, ErrClosed
	}
	b.libs[handle] = name
	b.mu.Unlock()
	return handle, nil
}

// Sym resolves a symbol in a library previously opened through this bridge.
func (b *Bridge) Sym(lib uintptr, name string) (uintptr, error) {
	if err := b.allow(OpSym, name); err != nil {
		return 0, err
	}
	if !Supported {
		return 0, ErrUnsupported
	}

	b.mu.Lock()
	libName, known := b.libs[lib]
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return 0, ErrClosed
	}
	if !known {
		return 0, fmt.Errorf("ffibridge: unknown library handle %#x", lib)
	}

	addr, err := dlSym(lib, name)
	if err != nil {
		return 0, fmt.Errorf("ffibridge: symbol %q not found in %q: %w", name, libName, err)
	}
	return addr, nil
}

// CloseLib unloads a single library.
func (b *Bridge) CloseLib(lib uintptr) error {
	b.mu.Lock()
	_, known := b.libs[lib]
	if known {
		delete(b.libs, lib)
		// Every cached callable holds a raw address, and unloading a library
		// invalidates the ones that came from it. Which callable came from
		// which library is not recorded, so the whole cache goes: rebuilding
		// an entry is cheap, calling into an unmapped page is not.
		b.callables = make(map[callKey]reflect.Value)
	}
	b.mu.Unlock()
	if !known {
		return fmt.Errorf("ffibridge: unknown library handle %#x", lib)
	}
	return dlClose(lib)
}

// Call invokes a native function described by a signature.
func (b *Bridge) Call(fn uintptr, sig string, args ...any) (any, error) {
	parsed, err := ParseSignature(sig)
	if err != nil {
		return nil, err
	}
	return b.CallSig(fn, parsed, args)
}

// CallSym resolves a symbol and calls it in one step.
func (b *Bridge) CallSym(lib uintptr, name, sig string, args ...any) (any, error) {
	fn, err := b.Sym(lib, name)
	if err != nil {
		return nil, err
	}
	return b.Call(fn, sig, args...)
}

// CallSig is Call with an already parsed signature, which avoids re-parsing on
// hot paths.
func (b *Bridge) CallSig(fn uintptr, sig *Signature, args []any) (result any, err error) {
	if sig == nil {
		return nil, errors.New("ffibridge: nil signature")
	}
	if err := b.allow(OpCall, sig.Text); err != nil {
		return nil, err
	}
	if fn == 0 {
		return nil, errors.New("ffibridge: null function pointer")
	}
	if b.isClosed() {
		return nil, ErrClosed
	}

	callable, err := b.callable(fn, sig)
	if err != nil {
		return nil, err
	}
	in, err := sig.convertArgs(args)
	if err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("ffibridge: call %s panicked: %v", sig.Text, r)
		}
	}()

	out := callable.Call(in)
	if sig.Ret == KindVoid {
		return nil, nil
	}
	return fromGo(sig.Ret, out[0]), nil
}

// callable returns the reflect function bound to fn for this signature,
// building and caching it on first use.
func (b *Bridge) callable(fn uintptr, sig *Signature) (reflect.Value, error) {
	key := callKey{fn: fn, sig: sig.Text}

	b.mu.Lock()
	cached, ok := b.callables[key]
	b.mu.Unlock()
	if ok {
		return cached, nil
	}

	built, err := makeCallable(sig.ftype, fn)
	if err != nil {
		return reflect.Value{}, err
	}

	b.mu.Lock()
	if existing, ok := b.callables[key]; ok {
		built = existing
	} else {
		b.callables[key] = built
	}
	b.mu.Unlock()
	return built, nil
}

// Close releases every library and memory block the bridge owns. Callbacks are
// intentionally kept alive: native code may still hold their addresses, and
// there is no portable way to revoke a trampoline.
func (b *Bridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	libs := make([]uintptr, 0, len(b.libs))
	for handle := range b.libs {
		libs = append(libs, handle)
	}
	b.libs = make(map[uintptr]string)
	b.blocks = make(map[uintptr][]byte)
	b.callables = make(map[callKey]reflect.Value)
	b.allocated = 0
	b.mu.Unlock()

	var firstErr error
	for _, handle := range libs {
		if err := dlClose(handle); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
