//go:build windows

// Win32 bindings. Deliberately no golang.org/x/sys dependency: the probe must
// build with a bare Go toolchain and no network ("go build ." in this folder).
package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	version  = syscall.NewLazyDLL("version.dll")

	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")

	procInitAttrList   = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateAttr     = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteAttrList = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW = kernel32.NewProc("CreateProcessW")

	procGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleTitleW           = kernel32.NewProc("GetConsoleTitleW")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleCP               = kernel32.NewProc("GetConsoleCP")
	procGetConsoleOutputCP         = kernel32.NewProc("GetConsoleOutputCP")
	procFlushConsoleInputBuffer    = kernel32.NewProc("FlushConsoleInputBuffer")
	procReadConsoleInputW          = kernel32.NewProc("ReadConsoleInputW")
	procGetNumberOfInputEvents     = kernel32.NewProc("GetNumberOfConsoleInputEvents")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetExitCodeProcess         = kernel32.NewProc("GetExitCodeProcess")
	procGetConsoleProcessList      = kernel32.NewProc("GetConsoleProcessList")

	procGetClassNameW           = user32.NewProc("GetClassNameW")
	procGetWindowLongPtrW       = user32.NewProc("GetWindowLongPtrW")
	procGetWindowRect           = user32.NewProc("GetWindowRect")
	procGetClientRect           = user32.NewProc("GetClientRect")
	procIsWindowVisible         = user32.NewProc("IsWindowVisible")
	procIsIconic                = user32.NewProc("IsIconic")
	procGetWindow               = user32.NewProc("GetWindow")
	procGetWindowThreadProcID   = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows             = user32.NewProc("EnumWindows")
	procGetWindowTextW          = user32.NewProc("GetWindowTextW")
	procGetDpiForWindow         = user32.NewProc("GetDpiForWindow")
	procGetForegroundWindow     = user32.NewProc("GetForegroundWindow")
	procRegGetValueW            = advapi32.NewProc("RegGetValueW")
	procRtlGetVersion           = ntdll.NewProc("RtlGetVersion")
	procGetFileVersionInfoSizeW = version.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = version.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = version.NewProc("VerQueryValueW")
	procGetProcessDpiAwareness  = syscall.NewLazyDLL("shcore.dll").NewProc("GetProcessDpiAwarenessInternal")
	_                           = procGetProcessDpiAwareness
)

const (
	stdInputHandle  = ^uintptr(9)  // -10
	stdOutputHandle = ^uintptr(10) // -11

	enableProcessedInput  = 0x0001
	enableLineInput       = 0x0002
	enableEchoInput       = 0x0004
	enableWindowInput     = 0x0008
	enableVTInput         = 0x0200
	enableVTProcessing    = 0x0004
	disableNewlineAutoRet = 0x0008

	gwlStyle   = ^uintptr(15) // -16
	gwlExStyle = ^uintptr(19) // -20
	gwlHwndPar = ^uintptr(7)  // -8
	gwOwner    = 4
	gwHwndPrev = 3

	wsClipChildren = 0x02000000
	wsVisible      = 0x10000000
	wsPopup        = 0x80000000
	wsExLayered    = 0x00080000
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008
	wsExNoActivate = 0x08000000

	extendedStartupInfoPresent = 0x00080000
	createUnicodeEnvironment   = 0x00000400
	createNewConsole           = 0x00000010
	attrPseudoConsole          = 0x00020016

	processQueryLimitedInfo = 0x1000
	processTerminate        = 0x0001

	hkeyCurrentUser  = 0x80000001
	hkeyLocalMachine = 0x80000002
)

type coord struct {
	X int16
	Y int16
}

func (c coord) pack() uintptr {
	return uintptr(uint32(uint16(c.X)) | uint32(uint16(c.Y))<<16)
}

type rect struct{ Left, Top, Right, Bottom int32 }

type smallRect struct{ Left, Top, Right, Bottom int16 }

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type inputRecord struct {
	EventType       uint16
	_               uint16
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
}

type startupInfoEx struct {
	si            syscall.StartupInfo
	attributeList uintptr
}

type osVersionInfoEx struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformId        uint32
	CSDVersion        [128]uint16
	ServicePackMajor  uint16
	ServicePackMinor  uint16
	SuiteMask         uint16
	ProductType       byte
	Reserved          byte
}

func getStdHandle(which uintptr) syscall.Handle {
	h, _ := syscall.GetStdHandle(int(int32(uint32(which))))
	return h
}

func getConsoleMode(h syscall.Handle) (uint32, bool) {
	var m uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&m)))
	return m, r != 0
}

func setConsoleMode(h syscall.Handle, m uint32) bool {
	r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(m))
	return r != 0
}

func consoleWindow() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

func className(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 128)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func windowText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func windowLong(hwnd, index uintptr) uintptr {
	v, _, _ := procGetWindowLongPtrW.Call(hwnd, index)
	return v
}

func windowRect(hwnd uintptr) rect {
	var r rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func clientRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func regString(sub, name string) string {
	return regStringAt(hkeyLocalMachine, sub, name)
}

func regStringAt(root uintptr, sub, name string) string {
	const rrfRtAny = 0x0000ffff
	buf := make([]uint16, 512)
	size := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ := procRegGetValueW.Call(
		root,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(sub))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))),
		uintptr(rrfRtAny),
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r != 0 {
		return ""
	}
	if typ == 4 { // REG_DWORD
		return itoa(int(*(*uint32)(unsafe.Pointer(&buf[0]))))
	}
	return syscall.UTF16ToString(buf)
}

type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

func fileVersion(path string) string {
	if path == "" {
		return "unknown"
	}
	p := syscall.StringToUTF16Ptr(path)
	var ignored uint32
	sz, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&ignored)))
	if sz == 0 {
		return "unknown"
	}
	buf := make([]byte, sz)
	ok, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(p)), 0, sz, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "unknown"
	}
	root := syscall.StringToUTF16Ptr(`\`)
	var ptr uintptr
	var n uint32
	ok, _, _ = procVerQueryValueW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || ptr == 0 || n < uint32(unsafe.Sizeof(vsFixedFileInfo{})) {
		return "unknown"
	}
	v := (*vsFixedFileInfo)(unsafe.Pointer(ptr))
	if v.Signature != 0xfeef04bd {
		return "unknown"
	}
	return itoa(int(v.FileVersionMS>>16)) + "." + itoa(int(v.FileVersionMS&0xffff)) + "." +
		itoa(int(v.FileVersionLS>>16)) + "." + itoa(int(v.FileVersionLS&0xffff))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func rtlGetVersion() osVersionInfoEx {
	var v osVersionInfoEx
	v.OSVersionInfoSize = uint32(unsafe.Sizeof(v))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	return v
}

func openProcess(access uint32, pid uint32) syscall.Handle {
	h, _, _ := syscall.Syscall(kernel32.NewProc("OpenProcess").Addr(), 3,
		uintptr(access), 0, uintptr(pid))
	return syscall.Handle(h)
}

func processImagePath(pid uint32) string {
	h := openProcess(processQueryLimitedInfo, pid)
	if h == 0 {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}
