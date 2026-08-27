//go:build windows

// f4imgprobe -- issue #805, and only issue #805.
//
// Two complaints, one shape: something repaints over the picture.
//
//   - classic conhost: the overlay window flickers and disappears. The console
//     window has no WS_CLIPCHILDREN, so every console repaint paints over a
//     child window of it and nothing invalidates the child afterwards
//     (docs/WINCON_805_HANDOVER.md F6). The proposed fix is an overlay that is
//     not a child at all: a top-level layered window that tracks the console
//     (step 3).
//   - Windows Terminal: the same build shows sixel sometimes and not others.
//     A sixel image in a text terminal lives in the text buffer, so the
//     suspicion is symmetrical -- writing text near or over the image erases
//     it, and f4 repaints its whole screen many times a second.
//
// So this probe does not survey the machine. It runs the two overlay
// mechanisms and the sixel path against the events suspected of erasing them,
// and asks the one question a program cannot answer for itself: is the picture
// still on the screen. Every answer is appended to the report immediately, so a
// run that is closed halfway is still worth attaching.
//
// Build: GOOS=windows GOARCH=amd64 go build -o f4imgprobe.exe ./tools/f4imgprobe
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	version  = syscall.NewLazyDLL("version.dll")

	procGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procFlushConsoleInputBuffer    = kernel32.NewProc("FlushConsoleInputBuffer")
	procReadConsoleInputW          = kernel32.NewProc("ReadConsoleInputW")
	procGetNumberOfConsoleInputEvs = kernel32.NewProc("GetNumberOfConsoleInputEvents")
	procGetCurrentConsoleFontEx    = kernel32.NewProc("GetCurrentConsoleFontEx")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procOpenProcess                = kernel32.NewProc("OpenProcess")

	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetWindow           = user32.NewProc("GetWindow")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procFillRect            = user32.NewProc("FillRect")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
	procInvalidateRect      = user32.NewProc("InvalidateRect")

	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")

	procRtlGetVersion           = ntdll.NewProc("RtlGetVersion")
	procGetFileVersionInfoSizeW = version.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = version.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = version.NewProc("VerQueryValueW")
)

const (
	stdInput  = ^uintptr(9)
	stdOutput = ^uintptr(10)

	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004
	enableVTProcessing   = 0x0004

	gwlExStyle = ^uintptr(19)
	gwHwndPrev = 3

	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsPopup         = 0x80000000
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000
	wsExTopmost     = 0x00000008

	swShowNoActivate = 4
	swHide           = 0
	swpNoMove        = 0x0002
	swpNoSize        = 0x0001
	swpNoActivate    = 0x0010

	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmNCHitTest   = 0x0084
	wmNull        = 0x0000
	htTransparent = ^uintptr(0) // (UINT_PTR)-1, the HTTRANSPARENT hit-test result

	pmRemove     = 0x0001
	acSrcOver    = 0x00
	acSrcAlpha   = 0x01
	ulwAlpha     = 0x0002
	dibRGBColors = 0
)

type point struct{ X, Y int32 }
type sizeT struct{ Cx, Cy int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type smallRect struct{ Left, Top, Right, Bottom int16 }
type coord struct{ X, Y int16 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type blendFunction struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

type consoleFontInfoEx struct {
	Size       uint32
	Font       uint32
	FontSize   coord
	FontFamily uint32
	FontWeight uint32
	FaceName   [32]uint16
}

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type osVersionInfoEx struct {
	OSVersionInfoSize                             uint32
	MajorVersion, MinorVersion, BuildNumber       uint32
	PlatformId                                    uint32
	CSDVersion                                    [128]uint16
	ServicePackMajor, ServicePackMinor, SuiteMask uint16
	ProductType, Reserved                         byte
}

// ---------------------------------------------------------------- reporting --

var reportFile *os.File

// report writes one line to the screen and to the report at the same time, and
// flushes: a run that is killed with the window's close button must still leave
// everything measured so far on disk.
func report(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Print(line + "\r\n")
	if reportFile != nil {
		reportFile.WriteString(line + "\r\n")
		reportFile.Sync()
	}
}

func section(title string) {
	report("")
	report("--- %s ---", title)
}

func main() {
	// Every window created here must live on the thread that pumps its
	// messages, and that pump runs between the questions on this thread.
	runtime.LockOSThread()

	exe, _ := os.Executable()
	path := "f4imgprobe-report.txt"
	if exe != "" {
		path = exe[:strings.LastIndexByte(exe, '\\')+1] + "f4imgprobe-report.txt"
	}
	f, err := os.Create(path)
	if err == nil {
		reportFile = f
		defer f.Close()
	}

	report("=== f4imgprobe 1 (issue #805: the picture disappears) ===")
	report("time: %s", time.Now().Format("2006-01-02 15:04:05 -0700"))
	report("report file: %s", path)
	fmt.Print("\r\nThis asks you a few times whether a picture is on the screen.\r\n")
	fmt.Print("Answer Y or N. Nothing is installed and nothing is changed.\r\n")

	describeHost()
	console := consoleWindow()
	class := classNameOf(console)

	if class == "ConsoleWindowClass" && isWindowVisible(console) {
		testConhostOverlays(console)
	} else {
		section("Overlay tests")
		report("skipped: this is not a classic console window (class %q), so an", class)
		report("overlay over the console handle cannot apply here. The picture on")
		report("this host has to travel as sixel, which the next section tests.")
	}

	testSixel()

	section("Done")
	report("Please attach %s to the issue.", path)
	fmt.Print("\r\nPress Enter to close. ")
	var dummy [1]byte
	os.Stdin.Read(dummy[:])
}

// ------------------------------------------------------------------- host ----

func describeHost() {
	section("Where this is running")
	v := osVersion()
	report("windows build: %d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	report("WT_SESSION: %s", envOr("WT_SESSION", "(not set)"))

	hwnd := consoleWindow()
	report("console window: %#x class %q visible=%v", hwnd, classNameOf(hwnd), isWindowVisible(hwnd))
	cr := clientRectOf(hwnd)
	report("console client area: %dx%d px", cr.Right-cr.Left, cr.Bottom-cr.Top)

	// The Windows Terminal build matters here: the complaint is that the same
	// version shows a picture sometimes and not others, so the exact file
	// version of the running terminal has to be in the report, not the name.
	if term := terminalProcessPath(); term != "" {
		report("terminal process: %s (file version %s)", term, fileVersion(term))
	}

	// The cell size decides the picture's geometry. Behind a pseudo console the
	// Win32 answer is a zero width, which is why f4 must ask the terminal
	// instead (handover F16/F17). Both are recorded so the field says which.
	var info consoleFontInfoEx
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(stdHandle(stdOutput)), 0,
		uintptr(unsafe.Pointer(&info))); r != 0 {
		report("GetCurrentConsoleFontEx cell: %dx%d px", info.FontSize.X, info.FontSize.Y)
		if info.FontSize.X <= 0 {
			report("  (a zero width: this host must not be asked for the cell this way)")
		}
	}
	withVT(func() {
		report("DA1: %s", escapeAnswer(ask("\x1b[c", 'c', 900*time.Millisecond)))
		report("cell size (CSI 16 t): %s", escapeAnswer(ask("\x1b[16t", 't', 900*time.Millisecond)))
		report("text area (CSI 14 t): %s", escapeAnswer(ask("\x1b[14t", 't', 900*time.Millisecond)))
		report("text area in cells (CSI 18 t): %s", escapeAnswer(ask("\x1b[18t", 't', 900*time.Millisecond)))
	})
}

// ------------------------------------------------------- conhost overlays ----

// testConhostOverlays runs both overlay mechanisms against the same event: a
// console repaint under the picture. That single comparison is the whole
// question behind "it flickers and disappears".
func testConhostOverlays(console uintptr) {
	section("Overlay test 1: a child window of the console (what f4 does today)")
	report("Expected to fail: the console window has no WS_CLIPCHILDREN, so its")
	report("own repaint paints over the child and nothing redraws the child after.")

	inst, _, _ := procGetModuleHandleW.Call(0)
	if !registerClass(inst, "f4imgprobeChild", childWndProc) {
		report("could not register a window class; overlay tests skipped")
		return
	}
	x, y, w, h := pictureRect(console)

	child, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("f4imgprobeChild"))), 0,
		uintptr(wsChild|wsVisible),
		uintptr(uint32(x)), uintptr(uint32(y)), uintptr(w), uintptr(h),
		console, 0, inst, 0)
	if child == 0 {
		report("CreateWindowEx (child of the console) failed")
	} else {
		report("created a child window of the console, %dx%d px at %d,%d", w, h, x, y)
		pump(400 * time.Millisecond)
		a1 := askYN("Do you see a red square inside the console window?")
		report("child overlay visible at first: %s", a1)

		// The event under suspicion: make the console repaint underneath it.
		fmt.Print("\r\n")
		for i := 0; i < 12; i++ {
			fmt.Printf("console repaint line %d\r\n", i)
		}
		pump(400 * time.Millisecond)
		a2 := askYN("Is the red square still there, whole and undamaged?")
		report("child overlay survives a console repaint: %s", a2)
		procDestroyWindow.Call(child)
		pump(100 * time.Millisecond)
	}

	section("Overlay test 2: a top-level layered window (the proposed fix)")
	report("No parent and no owner, positioned and filled in one")
	report("UpdateLayeredWindow call, then lifted just above the console.")

	if !registerClass(inst, "f4imgprobeLayered", defaultWndProc) {
		report("could not register the layered window class")
		return
	}
	top, _, _ := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTransparent|wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(utf16Ptr("f4imgprobeLayered"))), 0,
		uintptr(wsPopup),
		uintptr(uint32(x)), uintptr(uint32(y)), uintptr(w), uintptr(h),
		0, 0, inst, 0)
	if top == 0 {
		report("CreateWindowEx (top-level layered) failed")
		return
	}
	defer procDestroyWindow.Call(top)

	if !drawLayered(top, x, y, w, h) {
		report("UpdateLayeredWindow failed: this mechanism does not work on this build")
		return
	}
	procShowWindow.Call(top, swShowNoActivate)
	procSetWindowPos.Call(top, console, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpNoActivate))
	prev, _, _ := procGetWindow.Call(console, gwHwndPrev)
	report("placed above the console: %v", prev == top)
	pump(400 * time.Millisecond)

	b1 := askYN("Do you see a blue square over the console window?")
	report("layered overlay visible at first: %s", b1)

	fmt.Print("\r\n")
	for i := 0; i < 12; i++ {
		fmt.Printf("console repaint line %d\r\n", i)
	}
	pump(400 * time.Millisecond)
	b2 := askYN("Is the blue square still there, whole and undamaged?")
	report("layered overlay survives a console repaint: %s", b2)

	// The other half of the complaint is a freeze, and its suspected cause is
	// f4 waiting on the console's window thread. Time a call that would block
	// if conhost were wedged; a healthy console answers in single milliseconds.
	start := time.Now()
	var result uintptr
	procSendMessageTimeoutW.Call(console, wmNull, 0, 0, 0x0002 /*SMTO_ABORTIFHUNG*/, 2000,
		uintptr(unsafe.Pointer(&result)))
	report("console window answered a message in %v (a wedged console would take 2s)",
		time.Since(start).Round(time.Millisecond))

	fmt.Print("\r\nNow please MOVE the console window with the mouse, then come back.\r\n")
	b3 := askYN("After moving the window: did the blue square stay over the console?")
	report("layered overlay follows the console when moved: %s", b3)
	report("(f4 would run a tracking timer here; this probe deliberately does not,")
	report(" so a 'no' is expected and tells us the tracker is required, not optional.)")
	procShowWindow.Call(top, swHide)
}

// pictureRect is a square in the middle of the console's client area, in screen
// coordinates -- the same arithmetic the overlay does.
func pictureRect(console uintptr) (x, y, w, h int32) {
	cr := clientRectOf(console)
	origin := point{}
	procClientToScreen.Call(console, uintptr(unsafe.Pointer(&origin)))
	w, h = 160, 160
	if cr.Right-cr.Left < w*2 {
		w = (cr.Right - cr.Left) / 2
	}
	if cr.Bottom-cr.Top < h*2 {
		h = (cr.Bottom - cr.Top) / 2
	}
	return origin.X + (cr.Right-cr.Left-w)/2, origin.Y + (cr.Bottom-cr.Top-h)/2, w, h
}

var childWndProc = syscall.NewCallback(func(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		brush, _, _ := procCreateSolidBrush.Call(0x000000FF) // COLORREF is BGR: red
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&ps.RcPaint)), brush)
		procDeleteObject.Call(brush)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmEraseBkgnd:
		return 1
	case wmNCHitTest:
		return htTransparent
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
})

var defaultWndProc = syscall.NewCallback(func(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
})

func registerClass(inst uintptr, name string, proc uintptr) bool {
	wc := wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		WndProc:   proc,
		Instance:  inst,
		ClassName: utf16Ptr(name),
	}
	atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	return atom != 0
}

// drawLayered fills the window with a blue square in one UpdateLayeredWindow
// call: position, size and pixels together, which is what makes the "black
// rectangle before the first paint" impossible.
func drawLayered(hwnd uintptr, x, y, w, h int32) bool {
	screenDC, _, _ := procGetDC.Call(0)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	defer func() {
		procDeleteDC.Call(memDC)
		procReleaseDC.Call(0, screenDC)
	}()
	bih := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    w,
		Height:   -h, // top-down
		Planes:   1,
		BitCount: 32,
	}
	var bits unsafe.Pointer
	dib, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&bih)), dibRGBColors,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return false
	}
	defer procDeleteObject.Call(dib)
	px := unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	for i := 0; i < int(w)*int(h); i++ {
		// Opaque blue, premultiplied (alpha 255 leaves the values unchanged).
		px[i*4+0] = 230 // B
		px[i*4+1] = 40  // G
		px[i*4+2] = 20  // R
		px[i*4+3] = 255 // A
	}
	old, _, _ := procSelectObject.Call(memDC, dib)
	defer procSelectObject.Call(memDC, old)
	pt := point{X: x, Y: y}
	sz := sizeT{Cx: w, Cy: h}
	src := point{}
	bf := blendFunction{BlendOp: acSrcOver, SourceConstantAlpha: 255, AlphaFormat: acSrcAlpha}
	r, _, _ := procUpdateLayeredWindow.Call(hwnd, screenDC,
		uintptr(unsafe.Pointer(&pt)), uintptr(unsafe.Pointer(&sz)),
		memDC, uintptr(unsafe.Pointer(&src)), 0,
		uintptr(unsafe.Pointer(&bf)), ulwAlpha)
	return r != 0
}

// ------------------------------------------------------------------ sixel ----

// testSixel asks what erases a sixel image. The complaint is that the same
// Windows Terminal shows the picture sometimes and not others; if writing text
// erases it, then "sometimes" is simply "whenever f4 repainted next", which is
// a bug with an address rather than a mystery.
func testSixel() {
	section("Sixel test: does the picture survive what f4 does next")
	outMode, ok := consoleMode(stdOutput)
	if !ok {
		report("skipped: stdout is not a console")
		return
	}
	setConsoleMode(stdOutput, outMode|enableVTProcessing)
	defer setConsoleMode(stdOutput, outMode)

	fmt.Print("\x1b[2J\x1b[H")
	fmt.Print("A square should appear below: red on the left, blue on the right.\r\n\r\n")
	os.Stdout.WriteString(sixelSquare())
	fmt.Print("\r\n\r\n\r\n\r\n\r\n\r\n")
	s1 := askYN("Do you see the red and blue square?")
	report("sixel image appeared at all: %s", s1)
	if strings.HasPrefix(s1, "no") {
		report("(no image: either this terminal has no sixel support, or it is")
		report(" switched off in its settings -- both belong in the report.)")
		return
	}

	// 1. Text written somewhere else on the screen. This is the ordinary case
	//    for f4: the panels repaint constantly while the picture is shown.
	fmt.Print("text written below the image, nothing near it\r\n")
	pause(600 * time.Millisecond)
	s2 := askYN("Is the image still there after that line of text?")
	report("survives text written elsewhere: %s", s2)

	// 2. A repaint of the rows the image occupies, cursor moved with CUP and
	//    the line erased -- exactly what a full-screen redraw does.
	fmt.Print("\x1b[3;1H\x1b[K\x1b[4;1H\x1b[K")
	fmt.Print("\x1b[12;1H")
	pause(600 * time.Millisecond)
	s3 := askYN("Is the image still there after erasing two of its own lines?")
	report("survives an erase of its own rows (ESC[K): %s", s3)

	// 3. Scrolling: new output at the bottom pushes the image up.
	for i := 0; i < 3; i++ {
		fmt.Printf("scrolling line %d\r\n", i)
	}
	pause(600 * time.Millisecond)
	s4 := askYN("Is the image still there, moved up by three lines?")
	report("survives scrolling: %s", s4)

	// 4. The alternate screen. f4 uses it, and an image drawn on one buffer
	//    has no reason to exist on the other.
	fmt.Print("\x1b[?1049h\x1b[2J\x1b[H")
	fmt.Print("This is the alternate screen. The same image is drawn here.\r\n\r\n")
	os.Stdout.WriteString(sixelSquare())
	fmt.Print("\r\n\r\n\r\n\r\n\r\n\r\n")
	s5 := askYN("Do you see the square on this alternate screen?")
	fmt.Print("\x1b[?1049l")
	report("sixel works on the alternate screen: %s", s5)

	// 5. Redrawn repeatedly, which is what an animation or a repainting file
	//    manager does. Flicker here is the visible form of the complaint.
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Print("The image is now being redrawn 20 times in a row.\r\n\r\n")
	for i := 0; i < 20; i++ {
		fmt.Print("\x1b[3;1H")
		os.Stdout.WriteString(sixelSquare())
		time.Sleep(120 * time.Millisecond)
	}
	fmt.Print("\x1b[14;1H")
	s6 := askYN("During those redraws, did the image stay steady (Y) or blink (N)?")
	report("steady when redrawn 20 times: %s", s6)

	fmt.Print("\r\nNow please RESIZE the terminal window, then come back.\r\n")
	s7 := askYN("After resizing: is the image still on the screen?")
	report("survives a window resize: %s", s7)
}

// sixelSquare is a 90x90 square, red on the left half and blue on the right.
// Deliberately small: the point is whether it is there, not what it is.
func sixelSquare() string {
	var b strings.Builder
	b.WriteString("\x1bPq#0;2;90;10;10#1;2;10;10;90")
	for band := 0; band < 15; band++ { // 15 bands of 6 pixels = 90 tall
		b.WriteString("#0!45~$#1!45?!45~-")
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// ------------------------------------------------------------- asking you ----

// askYN pumps window messages while it waits, so the overlay windows created
// above stay alive and painted while the question is on the screen.
func askYN(question string) string {
	inMode, ok := consoleMode(stdInput)
	if !ok {
		return "not asked (no console input)"
	}
	setConsoleMode(stdInput, inMode&^(enableLineInput|enableEchoInput|enableProcessedInput))
	defer setConsoleMode(stdInput, inMode)
	procFlushConsoleInputBuffer.Call(stdHandle(stdInput))
	fmt.Print("\r\n" + question + " [Y/N] ")

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		pumpOnce()
		if ch := readCharNonBlocking(); ch != 0 {
			switch ch {
			case 'y', 'Y':
				fmt.Print("Y\r\n")
				return "yes"
			case 'n', 'N':
				fmt.Print("N\r\n")
				return "no"
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	fmt.Print("(no answer)\r\n")
	return "no answer within three minutes"
}

// pause keeps the message pump running instead of sleeping, so a window that
// needs repainting during the wait gets the chance.
func pause(d time.Duration) { pump(d) }

func pump(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		pumpOnce()
		time.Sleep(10 * time.Millisecond)
	}
}

func pumpOnce() {
	var m msg
	for {
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func readCharNonBlocking() rune {
	h := stdHandle(stdInput)
	var pending uint32
	if r, _, _ := procGetNumberOfConsoleInputEvs.Call(h, uintptr(unsafe.Pointer(&pending))); r == 0 || pending == 0 {
		return 0
	}
	var rec struct {
		EventType       uint16
		_               uint16
		KeyDown         int32
		RepeatCount     uint16
		VirtualKeyCode  uint16
		VirtualScanCode uint16
		UnicodeChar     uint16
		ControlKeyState uint32
	}
	var read uint32
	if r, _, _ := procReadConsoleInputW.Call(h, uintptr(unsafe.Pointer(&rec)), 1,
		uintptr(unsafe.Pointer(&read))); r == 0 || read == 0 {
		return 0
	}
	if rec.EventType != 1 || rec.KeyDown == 0 {
		return 0
	}
	return rune(rec.UnicodeChar)
}

// ask sends a VT query and reads the answer, with echo off so the reply does
// not appear on the screen.
func ask(query string, final byte, timeout time.Duration) string {
	procFlushConsoleInputBuffer.Call(stdHandle(stdInput))
	os.Stdout.WriteString(query)
	deadline := time.Now().Add(timeout)
	var sb strings.Builder
	for time.Now().Before(deadline) {
		ch := readCharNonBlocking()
		if ch == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		sb.WriteRune(ch)
		if byte(ch) == final && sb.Len() > 1 {
			return sb.String()
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	return sb.String()
}

// withVT turns on VT output and raw input for the duration of f, then puts both
// modes back exactly as they were.
func withVT(f func()) {
	inMode, inOK := consoleMode(stdInput)
	outMode, outOK := consoleMode(stdOutput)
	if !inOK || !outOK {
		report("VT queries skipped: this needs a console on both stdin and stdout")
		return
	}
	setConsoleMode(stdOutput, outMode|enableVTProcessing)
	setConsoleMode(stdInput, inMode&^(enableLineInput|enableEchoInput|enableProcessedInput))
	defer func() {
		setConsoleMode(stdInput, inMode)
		setConsoleMode(stdOutput, outMode)
	}()
	f()
}

// ----------------------------------------------------------------- plumbing --

func stdHandle(which uintptr) uintptr {
	h, _ := syscall.GetStdHandle(int(int32(uint32(which))))
	return uintptr(h)
}

func consoleMode(which uintptr) (uint32, bool) {
	var m uint32
	r, _, _ := procGetConsoleMode.Call(stdHandle(which), uintptr(unsafe.Pointer(&m)))
	return m, r != 0
}

func setConsoleMode(which uintptr, mode uint32) {
	procSetConsoleMode.Call(stdHandle(which), uintptr(mode))
}

func consoleWindow() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

func classNameOf(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 128)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func clientRectOf(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func osVersion() osVersionInfoEx {
	var v osVersionInfoEx
	v.OSVersionInfoSize = uint32(unsafe.Sizeof(v))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	return v
}

// terminalProcessPath finds the terminal drawing this console: for a pseudo
// console that is the owner window's process, which is Windows Terminal itself.
func terminalProcessPath() string {
	hwnd := consoleWindow()
	if hwnd == 0 {
		return ""
	}
	owner, _, _ := procGetWindow.Call(hwnd, 4 /*GW_OWNER*/)
	target := owner
	if target == 0 {
		target = hwnd
	}
	var pid uint32
	procGetWindowThreadPID.Call(target, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(0x1000 /*QUERY_LIMITED_INFORMATION*/, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(h, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

func fileVersion(path string) string {
	p := utf16Ptr(path)
	var ignored uint32
	sz, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&ignored)))
	if sz == 0 {
		return "unknown"
	}
	buf := make([]byte, sz)
	if ok, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(p)), 0, sz,
		uintptr(unsafe.Pointer(&buf[0]))); ok == 0 {
		return "unknown"
	}
	var ptr uintptr
	var n uint32
	root := utf16Ptr(`\`)
	if ok, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&n))); ok == 0 || ptr == 0 {
		return "unknown"
	}
	// The pointer VerQueryValue hands back points inside buf; read the fixed
	// info through the slice rather than through the raw address.
	off := int(ptr - uintptr(unsafe.Pointer(&buf[0])))
	if off < 0 || off+16 > len(buf) {
		return "unknown"
	}
	ms := uint32(buf[off+8]) | uint32(buf[off+9])<<8 | uint32(buf[off+10])<<16 | uint32(buf[off+11])<<24
	ls := uint32(buf[off+12]) | uint32(buf[off+13])<<8 | uint32(buf[off+14])<<16 | uint32(buf[off+15])<<24
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff)
}

func escapeAnswer(s string) string {
	if s == "" {
		return "(no answer)"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 0x1b:
			b.WriteString("ESC")
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "<%02X>", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func envOr(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}
