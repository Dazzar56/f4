package ffibridge

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Signature is one parsed C prototype written in the mini-language:
//
//	<ret>(<arg>, <arg>, ...)
//
// Examples:
//
//	i64(str)              size_t strlen(const char *)
//	ptr(ptr,ptr,i64)      void *memcpy(void *, const void *, size_t)
//	void(ptr,i64,i64,ptr) void qsort(void *, size_t, size_t, cmp *)
//	i32(ptr,str,...)      int sprintf(char *, const char *, ...)
//
// An empty or "void" argument list means no arguments. A trailing "..." marks
// a true C-variadic function; the types of the variadic arguments are then
// derived from the Go values passed at call time.
type Signature struct {
	Text     string
	Ret      Kind
	Args     []Kind
	Variadic bool

	ftype reflect.Type
}

var (
	sigMu    sync.RWMutex
	sigCache = make(map[string]*Signature)
)

// ParseSignature parses a signature and caches the result. Parsed signatures
// are immutable and safe for concurrent use.
func ParseSignature(text string) (*Signature, error) {
	sigMu.RLock()
	cached, ok := sigCache[text]
	sigMu.RUnlock()
	if ok {
		return cached, nil
	}

	sig, err := parseSignature(text)
	if err != nil {
		return nil, err
	}

	sigMu.Lock()
	sigCache[text] = sig
	sigMu.Unlock()
	return sig, nil
}

func parseSignature(text string) (*Signature, error) {
	body := strings.TrimSpace(text)
	open := strings.IndexByte(body, '(')
	if open < 0 || !strings.HasSuffix(body, ")") {
		return nil, fmt.Errorf("ffibridge: malformed signature %q, want ret(arg,arg)", text)
	}

	retName := strings.TrimSpace(body[:open])
	if retName == "" {
		retName = "void"
	}
	ret, ok := ParseKind(retName)
	if !ok {
		return nil, fmt.Errorf("ffibridge: unknown return type %q in %q", retName, text)
	}

	sig := &Signature{Text: text, Ret: ret}

	inner := strings.TrimSpace(body[open+1 : len(body)-1])
	if inner != "" && inner != "void" {
		for _, part := range strings.Split(inner, ",") {
			part = strings.TrimSpace(part)
			if part == "..." {
				if sig.Variadic {
					return nil, fmt.Errorf("ffibridge: %q: duplicate ...", text)
				}
				sig.Variadic = true
				continue
			}
			if sig.Variadic {
				return nil, fmt.Errorf("ffibridge: %q: no fixed argument may follow ...", text)
			}
			kind, ok := ParseKind(part)
			if !ok {
				return nil, fmt.Errorf("ffibridge: unknown argument type %q in %q", part, text)
			}
			if kind == KindVoid {
				return nil, fmt.Errorf("ffibridge: %q: void is not a valid argument type", text)
			}
			sig.Args = append(sig.Args, kind)
		}
	}

	sig.ftype = sig.buildFuncType()
	return sig, nil
}

func (s *Signature) buildFuncType() reflect.Type {
	in := make([]reflect.Type, 0, len(s.Args)+1)
	for _, arg := range s.Args {
		in = append(in, arg.reflectType())
	}
	if s.Variadic {
		in = append(in, reflect.SliceOf(anyType))
	}

	var out []reflect.Type
	if s.Ret != KindVoid {
		out = append(out, s.Ret.reflectType())
	}
	return reflect.FuncOf(in, out, s.Variadic)
}

// FuncType is the Go function type this signature is dispatched through.
func (s *Signature) FuncType() reflect.Type { return s.ftype }

// convertArgs turns sandbox-supplied values into the reflect values purego
// expects for this signature.
func (s *Signature) convertArgs(args []any) ([]reflect.Value, error) {
	if s.Variadic {
		if len(args) < len(s.Args) {
			return nil, fmt.Errorf("ffibridge: %s wants at least %d arguments, got %d", s.Text, len(s.Args), len(args))
		}
	} else if len(args) != len(s.Args) {
		return nil, fmt.Errorf("ffibridge: %s wants %d arguments, got %d", s.Text, len(s.Args), len(args))
	}

	in := make([]reflect.Value, 0, len(args))
	for i, kind := range s.Args {
		v, err := toGo(kind, args[i])
		if err != nil {
			return nil, fmt.Errorf("ffibridge: %s argument %d: %w", s.Text, i+1, err)
		}
		in = append(in, v)
	}
	for i, extra := range args[len(s.Args):] {
		v, err := variadicValue(extra)
		if err != nil {
			return nil, fmt.Errorf("ffibridge: %s variadic argument %d: %w", s.Text, i+1, err)
		}
		in = append(in, v)
	}
	return in, nil
}
