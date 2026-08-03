package ffibridge

import "runtime"

// LibCNames lists the usual names of the platform's C runtime, most likely
// first. It exists so that plugins and tests have one portable way to reach
// the functions everybody expects to be there.
func LibCNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"msvcrt.dll", "ucrtbase.dll"}
	case "darwin", "ios":
		return []string{"/usr/lib/libSystem.B.dylib", "libSystem.B.dylib"}
	case "linux", "android":
		return []string{"libc.so.6", "libc.so"}
	case "freebsd":
		return []string{"libc.so.7", "libc.so"}
	case "netbsd", "openbsd", "dragonfly":
		return []string{"libc.so", "libc.so.96"}
	}
	return []string{"libc.so"}
}

// OpenLibC opens the first C runtime it can find.
func (b *Bridge) OpenLibC() (uintptr, error) {
	var lastErr error
	for _, name := range LibCNames() {
		handle, err := b.Open(name)
		if err == nil {
			return handle, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrUnsupported
	}
	return 0, lastErr
}
