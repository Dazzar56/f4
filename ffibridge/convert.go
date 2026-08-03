package ffibridge

import (
	"fmt"
	"math"
	"reflect"
)

func typeError(k Kind, v any) error {
	return fmt.Errorf("cannot pass %T as %s", v, k)
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case uintptr:
		return int64(n), true
	case float32:
		return asInt64(float64(n))
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case uint64:
		return float64(n), true
	case uintptr:
		return float64(n), true
	}
	i, ok := asInt64(v)
	return float64(i), ok
}

func asUintptr(v any) (uintptr, bool) {
	if v == nil {
		return 0, true
	}
	if n, ok := v.(uintptr); ok {
		return n, true
	}
	if n, ok := v.(uint64); ok {
		return uintptr(n), true
	}
	n, ok := asInt64(v)
	if !ok {
		return 0, false
	}
	return uintptr(uint64(n)), true
}

func asString(v any) (string, bool) {
	switch s := v.(type) {
	case nil:
		return "", true
	case string:
		return s, true
	case []byte:
		return string(s), true
	}
	return "", false
}

// fitInt narrows a value to a signed integer kind, refusing to truncate.
func fitInt(n int64, bits uint) (int64, error) {
	if bits >= 64 {
		return n, nil
	}
	min := -(int64(1) << (bits - 1))
	max := (int64(1) << (bits - 1)) - 1
	if n < min || n > max {
		return 0, fmt.Errorf("value %d does not fit in i%d", n, bits)
	}
	return n, nil
}

// fitUint narrows a value to an unsigned integer kind. Negative values are
// accepted and wrapped, because writing -1 for an all-ones mask is idiomatic C.
func fitUint(n int64, bits uint) (uint64, error) {
	if bits >= 64 {
		return uint64(n), nil
	}
	mask := (uint64(1) << bits) - 1
	if n < 0 {
		if n < -(int64(1) << (bits - 1)) {
			return 0, fmt.Errorf("value %d does not fit in u%d", n, bits)
		}
		return uint64(n) & mask, nil
	}
	if uint64(n) > mask {
		return 0, fmt.Errorf("value %d does not fit in u%d", n, bits)
	}
	return uint64(n), nil
}

// toGo converts a sandbox value into the Go value purego passes to C.
func toGo(k Kind, v any) (reflect.Value, error) {
	switch k {
	case KindBool:
		if b, ok := v.(bool); ok {
			return reflect.ValueOf(b), nil
		}
		if v == nil {
			return reflect.ValueOf(false), nil
		}
		n, ok := asInt64(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		return reflect.ValueOf(n != 0), nil

	case KindI8, KindI16, KindI32, KindI64:
		if v == nil {
			v = 0
		}
		raw, ok := asInt64(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		n, err := fitInt(raw, k.bits())
		if err != nil {
			return reflect.Value{}, err
		}
		switch k {
		case KindI8:
			return reflect.ValueOf(int8(n)), nil
		case KindI16:
			return reflect.ValueOf(int16(n)), nil
		case KindI32:
			return reflect.ValueOf(int32(n)), nil
		}
		return reflect.ValueOf(n), nil

	case KindU8, KindU16, KindU32, KindU64:
		if v == nil {
			v = 0
		}
		raw, ok := asInt64(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		n, err := fitUint(raw, k.bits())
		if err != nil {
			return reflect.Value{}, err
		}
		switch k {
		case KindU8:
			return reflect.ValueOf(uint8(n)), nil
		case KindU16:
			return reflect.ValueOf(uint16(n)), nil
		case KindU32:
			return reflect.ValueOf(uint32(n)), nil
		}
		return reflect.ValueOf(n), nil

	case KindF32, KindF64:
		if v == nil {
			v = 0
		}
		f, ok := asFloat64(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		if k == KindF32 {
			return reflect.ValueOf(float32(f)), nil
		}
		return reflect.ValueOf(f), nil

	case KindPtr:
		p, ok := asUintptr(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		return reflect.ValueOf(p), nil

	case KindStr:
		s, ok := asString(v)
		if !ok {
			return reflect.Value{}, typeError(k, v)
		}
		return reflect.ValueOf(s), nil
	}
	return reflect.Value{}, fmt.Errorf("ffibridge: %s is not a usable value type", k)
}

// fromGo normalises a C return value into the small set of types the sandbox
// bindings understand: bool, int64, uint64, float64, uintptr, string.
func fromGo(k Kind, v reflect.Value) any {
	switch k {
	case KindBool:
		return v.Bool()
	case KindI8, KindI16, KindI32, KindI64:
		return v.Int()
	case KindU8, KindU16, KindU32, KindU64:
		return v.Uint()
	case KindF32, KindF64:
		return v.Float()
	case KindPtr:
		return uintptr(v.Uint())
	case KindStr:
		return v.String()
	}
	return nil
}

// variadicValue picks the ABI slot type for an argument passed after "...".
// Plain integers become int32 when they fit, matching C's default promotion of
// int; pass an explicit int64, float64 or uintptr to control the slot yourself.
func variadicValue(v any) (reflect.Value, error) {
	switch n := v.(type) {
	case nil:
		return reflect.ValueOf(uintptr(0)), nil
	case bool:
		if n {
			return reflect.ValueOf(int32(1)), nil
		}
		return reflect.ValueOf(int32(0)), nil
	case string:
		return reflect.ValueOf(n), nil
	case []byte:
		return reflect.ValueOf(string(n)), nil
	case float32:
		return reflect.ValueOf(float64(n)), nil
	case float64:
		return reflect.ValueOf(n), nil
	case int64:
		return reflect.ValueOf(n), nil
	case uint64:
		return reflect.ValueOf(n), nil
	case uintptr:
		return reflect.ValueOf(n), nil
	}

	raw, ok := asInt64(v)
	if !ok {
		return reflect.Value{}, fmt.Errorf("cannot pass %T as a variadic argument", v)
	}
	if raw >= math.MinInt32 && raw <= math.MaxInt32 {
		return reflect.ValueOf(int32(raw)), nil
	}
	return reflect.ValueOf(raw), nil
}
