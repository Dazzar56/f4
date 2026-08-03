package ffibridge

import "reflect"

// Kind is one primitive type of the ffibridge signature mini-language.
//
// The language deliberately has no C type names, no typedefs and no
// declaration parser: a sandboxed plugin describes the ABI it wants, not the
// C source it came from. A cdef-style parser may be layered on top later.
type Kind uint8

const (
	KindVoid Kind = iota
	KindBool
	KindI8
	KindU8
	KindI16
	KindU16
	KindI32
	KindU32
	KindI64
	KindU64
	KindF32
	KindF64
	KindPtr
	KindStr
)

var kindNames = [...]string{
	KindVoid: "void",
	KindBool: "bool",
	KindI8:   "i8",
	KindU8:   "u8",
	KindI16:  "i16",
	KindU16:  "u16",
	KindI32:  "i32",
	KindU32:  "u32",
	KindI64:  "i64",
	KindU64:  "u64",
	KindF32:  "f32",
	KindF64:  "f64",
	KindPtr:  "ptr",
	KindStr:  "str",
}

var kindByName = func() map[string]Kind {
	m := make(map[string]Kind, len(kindNames))
	for i, n := range kindNames {
		m[n] = Kind(i)
	}
	return m
}()

var anyType = reflect.TypeOf((*any)(nil)).Elem()

var kindTypes = [...]reflect.Type{
	KindVoid: nil,
	KindBool: reflect.TypeOf(false),
	KindI8:   reflect.TypeOf(int8(0)),
	KindU8:   reflect.TypeOf(uint8(0)),
	KindI16:  reflect.TypeOf(int16(0)),
	KindU16:  reflect.TypeOf(uint16(0)),
	KindI32:  reflect.TypeOf(int32(0)),
	KindU32:  reflect.TypeOf(uint32(0)),
	KindI64:  reflect.TypeOf(int64(0)),
	KindU64:  reflect.TypeOf(uint64(0)),
	KindF32:  reflect.TypeOf(float32(0)),
	KindF64:  reflect.TypeOf(float64(0)),
	KindPtr:  reflect.TypeOf(uintptr(0)),
	KindStr:  reflect.TypeOf(""),
}

// String returns the name this kind has inside a signature.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "invalid"
}

// ParseKind resolves a type name used in a signature.
func ParseKind(name string) (Kind, bool) {
	k, ok := kindByName[name]
	return k, ok
}

// reflectType is the Go type a value of this kind is passed to purego as.
func (k Kind) reflectType() reflect.Type {
	if int(k) < len(kindTypes) {
		return kindTypes[k]
	}
	return nil
}

// bits is the width of an integer kind, in bits.
func (k Kind) bits() uint {
	switch k {
	case KindI8, KindU8:
		return 8
	case KindI16, KindU16:
		return 16
	case KindI32, KindU32:
		return 32
	}
	return 64
}
