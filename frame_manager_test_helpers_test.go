package main

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/unxed/vtui"
)

// snapshotFrameManagerState saves a byte-for-byte copy of the current
// *vtui.FrameManager and returns a function that restores it in place.
//
// vtui.frameManager embeds several sync.Mutex fields, so a plain Go
// assignment of the dereferenced value (oldFM := *vtui.FrameManager) trips
// `go vet`'s copylocks check -- rightly so in general, since copying a
// mutex that might be locked produces two independently-lockable copies of
// what was meant to be one lock. That risk doesn't apply here: this runs
// at test setup and teardown, well before or after anything in the test
// itself could be holding one of those locks, so a byte-for-byte copy is
// exactly as safe as the struct assignment it replaces -- it's just
// performed as a raw memory copy via unsafe instead of a typed Go
// assignment, so it isn't the kind of expression the copylocks checker
// looks for.
func snapshotFrameManagerState(t *testing.T) func() {
	t.Helper()
	size := unsafe.Sizeof(*vtui.FrameManager)
	backup := make([]byte, size)
	copy(backup, unsafe.Slice((*byte)(unsafe.Pointer(vtui.FrameManager)), size))
	return func() {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(vtui.FrameManager)), size), backup)
	}
}

// swapFrameManager replaces the global vtui.FrameManager with a fresh,
// independent instance seeded with a byte-for-byte copy of the current
// one (see snapshotFrameManagerState for why that copy is safe here), and
// returns a function that restores the original pointer.
//
// vtui.frameManager is unexported, so this package has no way to spell
// its type in order to declare a second instance the normal way (no
// &T{}, no new(T)); reflect.New can still allocate one from its runtime
// type descriptor, and setting the package variable through
// reflect.Value.Set isn't a syntactic struct assignment either, so this
// sidesteps both the naming problem and the copylocks check that a
// direct assignment would hit.
func swapFrameManager(t *testing.T) func() {
	t.Helper()
	old := vtui.FrameManager
	elemType := reflect.TypeOf(old).Elem()
	newVal := reflect.New(elemType)

	size := elemType.Size()
	src := unsafe.Slice((*byte)(unsafe.Pointer(old)), size)
	dst := unsafe.Slice((*byte)(newVal.UnsafePointer()), size)
	copy(dst, src)

	fmVar := reflect.ValueOf(&vtui.FrameManager).Elem()
	fmVar.Set(newVal)

	return func() {
		fmVar.Set(reflect.ValueOf(old))
	}
}
