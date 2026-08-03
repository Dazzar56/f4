package ffibridge

import (
	"errors"
	"fmt"
	"reflect"
)

// Callback is the sandbox-facing shape of a native callback body. Arguments
// arrive normalised the same way call results are, and the returned value is
// converted back according to the signature's return type.
type Callback func(args []any) (any, error)

// NewCallback builds a native function pointer that dispatches into fn. The
// address stays valid for the lifetime of the bridge; there is no portable way
// to revoke a trampoline, so callbacks are never reclaimed early.
func (b *Bridge) NewCallback(sig string, fn Callback) (uintptr, error) {
	parsed, err := ParseSignature(sig)
	if err != nil {
		return 0, err
	}
	if err := b.allow(OpCallback, parsed.Text); err != nil {
		return 0, err
	}
	if !Supported {
		return 0, ErrUnsupported
	}
	if parsed.Variadic {
		return 0, errors.New("ffibridge: variadic callbacks are not supported")
	}
	if fn == nil {
		return 0, errors.New("ffibridge: nil callback body")
	}
	if b.isClosed() {
		return 0, ErrClosed
	}

	impl := reflect.MakeFunc(parsed.ftype, func(in []reflect.Value) []reflect.Value {
		args := make([]any, len(in))
		for i, v := range in {
			args[i] = fromGo(parsed.Args[i], v)
		}

		out, callErr := invokeCallback(fn, args)
		if parsed.Ret == KindVoid {
			return nil
		}
		if callErr != nil {
			return []reflect.Value{reflect.Zero(parsed.Ret.reflectType())}
		}
		converted, convErr := toGo(parsed.Ret, out)
		if convErr != nil {
			return []reflect.Value{reflect.Zero(parsed.Ret.reflectType())}
		}
		return []reflect.Value{converted}
	})

	body := impl.Interface()
	addr, err := makeCallback(body)
	if err != nil {
		return 0, err
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, ErrClosed
	}
	b.callbacks[addr] = body
	b.mu.Unlock()
	return addr, nil
}

// invokeCallback isolates the guest body: a panic inside a sandbox must not
// unwind through native frames, so it is turned into an error and the callback
// returns a zero value instead.
func invokeCallback(fn Callback, args []any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("ffibridge: callback panicked: %v", r)
		}
	}()
	return fn(args)
}
