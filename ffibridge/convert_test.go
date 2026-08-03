package ffibridge

import (
	"reflect"
	"testing"
)

func TestToGoNumericCoercion(t *testing.T) {
	cases := []struct {
		kind Kind
		in   any
		want any
	}{
		{KindI32, 42, int32(42)},
		{KindI32, float64(42), int32(42)},
		{KindI32, float64(42.9), int32(42)},
		{KindI64, uint32(7), int64(7)},
		{KindU8, 255, uint8(255)},
		{KindU32, -1, uint32(0xFFFFFFFF)},
		{KindU64, 5, uint64(5)},
		{KindF32, 1, float32(1)},
		{KindF64, "", nil},
		{KindBool, 1, true},
		{KindBool, 0, false},
		{KindBool, nil, false},
		{KindPtr, nil, uintptr(0)},
		{KindPtr, 0x1000, uintptr(0x1000)},
		{KindStr, nil, ""},
		{KindStr, []byte("abc"), "abc"},
	}

	for _, tc := range cases {
		got, err := toGo(tc.kind, tc.in)
		if tc.want == nil {
			if err == nil {
				t.Errorf("toGo(%v, %#v) accepted an invalid value", tc.kind, tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("toGo(%v, %#v): %v", tc.kind, tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), tc.want) {
			t.Errorf("toGo(%v, %#v) = %#v, want %#v", tc.kind, tc.in, got.Interface(), tc.want)
		}
	}
}

func TestToGoRejectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		in   any
	}{
		{KindI8, 200},
		{KindU8, 256},
		{KindU8, -200},
		{KindI16, 40000},
		{KindU32, int64(1) << 40},
	} {
		if _, err := toGo(tc.kind, tc.in); err == nil {
			t.Errorf("toGo(%v, %v) accepted an out of range value", tc.kind, tc.in)
		}
	}
}

func TestFromGoNormalisation(t *testing.T) {
	if got := fromGo(KindI32, reflect.ValueOf(int32(-5))); got != int64(-5) {
		t.Errorf("i32 result = %#v, want int64(-5)", got)
	}
	if got := fromGo(KindU16, reflect.ValueOf(uint16(9))); got != uint64(9) {
		t.Errorf("u16 result = %#v, want uint64(9)", got)
	}
	if got := fromGo(KindF32, reflect.ValueOf(float32(0.5))); got != float64(0.5) {
		t.Errorf("f32 result = %#v, want float64(0.5)", got)
	}
	if got := fromGo(KindPtr, reflect.ValueOf(uintptr(16))); got != uintptr(16) {
		t.Errorf("ptr result = %#v, want uintptr(16)", got)
	}
	if got := fromGo(KindStr, reflect.ValueOf("hi")); got != "hi" {
		t.Errorf("str result = %#v, want \"hi\"", got)
	}
}

func TestVariadicSlotTypes(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{42, int32(42)},
		{int64(1) << 40, int64(1) << 40},
		{2.5, float64(2.5)},
		{"text", "text"},
		{true, int32(1)},
		{uintptr(8), uintptr(8)},
		{nil, uintptr(0)},
	}
	for _, tc := range cases {
		got, err := variadicValue(tc.in)
		if err != nil {
			t.Errorf("variadicValue(%#v): %v", tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), tc.want) {
			t.Errorf("variadicValue(%#v) = %#v, want %#v", tc.in, got.Interface(), tc.want)
		}
	}
}
