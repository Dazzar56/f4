package ffibridge

import (
	"errors"
	"testing"
	"unsafe"
)

// testLibC opens the platform C runtime, skipping when the build or the system
// cannot provide one.
func testLibC(t *testing.T) (*Bridge, uintptr) {
	t.Helper()
	if !Supported {
		t.Skip("ffibridge: FFI is disabled in this build")
	}
	b := New(Options{})
	t.Cleanup(func() { _ = b.Close() })

	lib, err := b.OpenLibC()
	if err != nil {
		t.Skipf("no system C library available: %v", err)
	}
	return b, lib
}

func requireSym(t *testing.T, b *Bridge, lib uintptr, name string) uintptr {
	t.Helper()
	fn, err := b.Sym(lib, name)
	if err != nil {
		t.Skipf("symbol %q unavailable here: %v", name, err)
	}
	return fn
}

func TestCallIntegerReturn(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "strlen")

	got, err := b.CallSym(lib, "strlen", "i64(str)", "hello")
	if err != nil {
		t.Fatalf("strlen: %v", err)
	}
	if got != int64(5) {
		t.Fatalf("strlen(\"hello\") = %#v, want int64(5)", got)
	}
}

func TestCallNarrowIntegerReturn(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "abs")

	got, err := b.CallSym(lib, "abs", "i32(i32)", -42)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got != int64(42) {
		t.Fatalf("abs(-42) = %#v, want int64(42)", got)
	}
}

func TestCallFloatReturn(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "atof")

	got, err := b.CallSym(lib, "atof", "f64(str)", "2.5")
	if err != nil {
		t.Fatalf("atof: %v", err)
	}
	if got != float64(2.5) {
		t.Fatalf("atof(\"2.5\") = %#v, want float64(2.5)", got)
	}
}

func TestCallStringReturn(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "strstr")

	got, err := b.CallSym(lib, "strstr", "str(str,str)", "hello world", "world")
	if err != nil {
		t.Fatalf("strstr: %v", err)
	}
	if got != "world" {
		t.Fatalf("strstr = %#v, want \"world\"", got)
	}
}

func TestCallWithBridgeMemory(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "memcpy")

	src, err := b.CString("payload")
	if err != nil {
		t.Fatalf("CString: %v", err)
	}
	dst, err := b.Alloc(8)
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}

	got, err := b.CallSym(lib, "memcpy", "ptr(ptr,ptr,i64)", dst, src, 8)
	if err != nil {
		t.Fatalf("memcpy: %v", err)
	}
	if got != dst {
		t.Fatalf("memcpy returned %#v, want the destination %#x", got, dst)
	}

	copied, err := b.GoStringAt(dst)
	if err != nil {
		t.Fatalf("GoStringAt: %v", err)
	}
	if copied != "payload" {
		t.Fatalf("copied string = %q, want \"payload\"", copied)
	}
}

func TestCallbackThroughQsort(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "qsort")

	const count = 4
	block, err := b.Alloc(count * 4)
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	raw, err := b.Bytes(block)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	for i, v := range []int32{4, 1, 3, 2} {
		*(*int32)(unsafe.Pointer(&raw[i*4])) = v
	}

	calls := 0
	compare, err := b.NewCallback("i32(ptr,ptr)", func(args []any) (any, error) {
		calls++
		left := *(*int32)(unsafe.Pointer(args[0].(uintptr)))
		right := *(*int32)(unsafe.Pointer(args[1].(uintptr)))
		switch {
		case left < right:
			return -1, nil
		case left > right:
			return 1, nil
		}
		return 0, nil
	})
	if err != nil {
		t.Fatalf("NewCallback: %v", err)
	}

	if _, err := b.CallSym(lib, "qsort", "void(ptr,i64,i64,ptr)", block, count, 4, compare); err != nil {
		t.Fatalf("qsort: %v", err)
	}
	if calls == 0 {
		t.Fatal("qsort never invoked the comparator")
	}
	for i, want := range []int32{1, 2, 3, 4} {
		if got := *(*int32)(unsafe.Pointer(&raw[i*4])); got != want {
			t.Fatalf("element %d = %d, want %d", i, got, want)
		}
	}
}

func TestVariadicCall(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "sprintf")

	buf, err := b.Alloc(64)
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	if _, err := b.CallSym(lib, "sprintf", "i32(ptr,str,...)", buf, "%d-%s", 42, "ok"); err != nil {
		t.Fatalf("sprintf: %v", err)
	}

	got, err := b.GoStringAt(buf)
	if err != nil {
		t.Fatalf("GoStringAt: %v", err)
	}
	if got != "42-ok" {
		t.Fatalf("sprintf wrote %q, want \"42-ok\"", got)
	}
}

func TestCallErrors(t *testing.T) {
	b, lib := testLibC(t)

	if _, err := b.Call(0, "i32()"); err == nil {
		t.Error("calling a null pointer was allowed")
	}
	if _, err := b.Call(0, "nope()"); err == nil {
		t.Error("an invalid signature was accepted")
	}
	if _, err := b.Sym(lib+1, "strlen"); err == nil {
		t.Error("an unknown library handle was accepted")
	}
	if _, err := b.Sym(lib, "f4_no_such_symbol_here"); err == nil {
		t.Error("a missing symbol was accepted")
	}

	fn := requireSym(t, b, lib, "strlen")
	if _, err := b.Call(fn, "i64(str)"); err == nil {
		t.Error("a call with too few arguments was allowed")
	}
}

func TestCloseLibDropsCachedCallables(t *testing.T) {
	b, lib := testLibC(t)
	requireSym(t, b, lib, "strlen")

	if _, err := b.CallSym(lib, "strlen", "i64(str)", "hello"); err != nil {
		t.Fatalf("strlen: %v", err)
	}

	b.mu.Lock()
	cached := len(b.callables)
	b.mu.Unlock()
	if cached == 0 {
		t.Fatal("nothing was cached, so this test would prove nothing")
	}

	if err := b.CloseLib(lib); err != nil {
		t.Fatalf("CloseLib: %v", err)
	}

	// A cached callable is bound to a raw address inside the library. Keeping
	// one past the unload would leave a call jumping into an unmapped page.
	b.mu.Lock()
	left := len(b.callables)
	b.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d callable(s) still point into the unloaded library", left)
	}

	if err := b.CloseLib(lib); err == nil {
		t.Error("closing an unknown library handle was accepted")
	}
}

func TestPermissionHookBlocksOpen(t *testing.T) {
	denied := errors.New("denied by policy")
	b := New(Options{Allow: func(op Op, detail string) error {
		if op == OpOpen {
			return denied
		}
		return nil
	}})
	defer b.Close()

	if _, err := b.Open("libc.so.6"); !errors.Is(err, denied) {
		t.Fatalf("Open = %v, want the policy error", err)
	}
}

func TestClosedBridgeRejectsCalls(t *testing.T) {
	b, lib := testLibC(t)
	fn := requireSym(t, b, lib, "strlen")

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := b.Call(fn, "i64(str)", "hello"); !errors.Is(err, ErrClosed) {
		t.Errorf("Call after Close = %v, want ErrClosed", err)
	}
	if _, err := b.Sym(lib, "strlen"); !errors.Is(err, ErrClosed) {
		t.Errorf("Sym after Close = %v, want ErrClosed", err)
	}
}
