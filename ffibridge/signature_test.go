package ffibridge

import (
	"reflect"
	"testing"
)

func TestParseSignatureValid(t *testing.T) {
	cases := []struct {
		text     string
		ret      Kind
		args     []Kind
		variadic bool
	}{
		{"void()", KindVoid, nil, false},
		{"void(void)", KindVoid, nil, false},
		{"i64(str)", KindI64, []Kind{KindStr}, false},
		{"ptr(ptr,ptr,i64)", KindPtr, []Kind{KindPtr, KindPtr, KindI64}, false},
		{" f64 ( f32 , u8 ) ", KindF64, []Kind{KindF32, KindU8}, false},
		{"i32(ptr,str,...)", KindI32, []Kind{KindPtr, KindStr}, true},
		{"(i32)", KindVoid, []Kind{KindI32}, false},
	}

	for _, tc := range cases {
		sig, err := ParseSignature(tc.text)
		if err != nil {
			t.Fatalf("ParseSignature(%q): %v", tc.text, err)
		}
		if sig.Ret != tc.ret {
			t.Errorf("%q: return kind = %v, want %v", tc.text, sig.Ret, tc.ret)
		}
		if !reflect.DeepEqual(sig.Args, tc.args) {
			t.Errorf("%q: args = %v, want %v", tc.text, sig.Args, tc.args)
		}
		if sig.Variadic != tc.variadic {
			t.Errorf("%q: variadic = %v, want %v", tc.text, sig.Variadic, tc.variadic)
		}
	}
}

func TestParseSignatureInvalid(t *testing.T) {
	for _, text := range []string{
		"",
		"i32",
		"i32(",
		"i32(i32",
		"nope(i32)",
		"i32(nope)",
		"i32(void,i32)",
		"i32(...,i32)",
		"i32(...,...)",
	} {
		if _, err := ParseSignature(text); err == nil {
			t.Errorf("ParseSignature(%q) accepted an invalid signature", text)
		}
	}
}

func TestParseSignatureIsCached(t *testing.T) {
	first, err := ParseSignature("u32(u16,f32)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	second, err := ParseSignature("u32(u16,f32)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	if first != second {
		t.Fatal("ParseSignature returned a fresh signature instead of the cached one")
	}
}

func TestSignatureFuncType(t *testing.T) {
	sig, err := ParseSignature("f64(ptr,i32)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	want := reflect.TypeOf(func(uintptr, int32) float64 { return 0 })
	if sig.FuncType() != want {
		t.Fatalf("FuncType = %v, want %v", sig.FuncType(), want)
	}

	variadic, err := ParseSignature("i32(str,...)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	if !variadic.FuncType().IsVariadic() {
		t.Fatal("variadic signature produced a non-variadic func type")
	}
}

func TestConvertArgsArity(t *testing.T) {
	sig, err := ParseSignature("i32(i32,i32)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	if _, err := sig.convertArgs([]any{1}); err == nil {
		t.Error("convertArgs accepted too few arguments")
	}
	if _, err := sig.convertArgs([]any{1, 2, 3}); err == nil {
		t.Error("convertArgs accepted too many arguments")
	}
	if _, err := sig.convertArgs([]any{1, 2}); err != nil {
		t.Errorf("convertArgs rejected a valid call: %v", err)
	}

	variadic, err := ParseSignature("i32(i32,...)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	if _, err := variadic.convertArgs([]any{1, "x", 2.5}); err != nil {
		t.Errorf("convertArgs rejected a valid variadic call: %v", err)
	}
	if _, err := variadic.convertArgs(nil); err == nil {
		t.Error("convertArgs accepted a variadic call without its fixed argument")
	}
}
