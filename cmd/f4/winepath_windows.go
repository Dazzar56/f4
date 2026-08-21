//go:build windows && (amd64 || arm64)

package main

import (
	"syscall"
	"unsafe"
)

// Wine is the only thing that knows how the prefix maps drives onto the
// file system, and it exports that knowledge from ntdll for exactly this
// purpose. Guessing instead is what breaks drag and drop: "Z:\" is merely
// the usual spelling of "/", and a payload can just as well arrive as
// "C:\users\...", or through "D:" that the user pointed at /mnt/data
// himself, or as "\\?\unix\..." when no drive covers the file at all. A
// string rule can enumerate the spellings it has seen; only Wine knows the
// mapping.
//
// Both exports are cdecl. That only matters on 32-bit x86, where the
// caller cleans the stack and syscall.LazyProc does not; on the amd64 and
// arm64 builds this file is limited to there is one convention and the
// distinction disappears. On real Windows the exports are absent, Find
// fails, and every function here reports false so the caller keeps its own
// handling.
var (
	ntdllDLL                = syscall.NewLazyDLL("ntdll.dll")
	procWineGetUnixFileName = ntdllDLL.NewProc("wine_get_unix_file_name")
	procWineGetDosFileName  = ntdllDLL.NewProc("wine_get_dos_file_name")

	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	procGetProcessHeap = kernel32DLL.NewProc("GetProcessHeap")
	procHeapFree       = kernel32DLL.NewProc("HeapFree")
)

// hostUnixPath translates a DOS path into the POSIX path it names. The
// second result is false when the translation is unavailable -- on real
// Windows, or for a path no drive covers.
func hostUnixPath(dos string) (string, bool) {
	if dos == "" || procWineGetUnixFileName.Find() != nil {
		return "", false
	}
	in, err := syscall.UTF16PtrFromString(dos)
	if err != nil {
		return "", false
	}
	out, _, _ := procWineGetUnixFileName.Call(uintptr(unsafe.Pointer(in)))
	if out == 0 {
		return "", false
	}
	defer freeProcessHeap(out)
	return stringFromCString(out), true
}

// hostDosPath is the other direction: the DOS path a Wine application can
// open, for a POSIX path we already hold.
func hostDosPath(unix string) (string, bool) {
	if unix == "" || procWineGetDosFileName.Find() != nil {
		return "", false
	}
	in, err := syscall.BytePtrFromString(unix)
	if err != nil {
		return "", false
	}
	out, _, _ := procWineGetDosFileName.Call(uintptr(unsafe.Pointer(in)))
	if out == 0 {
		return "", false
	}
	defer freeProcessHeap(out)
	return stringFromWideCString(out), true
}

// freeProcessHeap releases a buffer Wine allocated for us. Both exports
// hand back memory from the process heap and leave freeing it to the
// caller.
func freeProcessHeap(p uintptr) {
	if p == 0 {
		return
	}
	heap, _, _ := procGetProcessHeap.Call()
	if heap == 0 {
		return
	}
	procHeapFree.Call(heap, 0, p)
}

// stringFromCString reads a NUL-terminated byte string. Wine returns unix
// paths in UTF-8, which is what Go strings already hold.
func stringFromCString(p uintptr) string {
	var b []byte
	for i := uintptr(0); i < 32*1024; i++ {
		c := *(*byte)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// stringFromWideCString reads a NUL-terminated UTF-16 string.
func stringFromWideCString(p uintptr) string {
	var u []uint16
	for i := uintptr(0); i < 32*1024; i++ {
		c := *(*uint16)(unsafe.Pointer(p + i*2))
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u)
}
