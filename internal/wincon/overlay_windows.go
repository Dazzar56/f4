//go:build windows

package wincon

// A picture over a classic Windows console.
//
// Windows Terminal renders sixel itself, so nothing here runs there and
// nothing should: pictures go down the wire as they do on any capable
// terminal. This is for conhost — cmd.exe in its own window — which has no
// image protocol of any kind and never will.
//
// The shape is the same as the X side, and for the same reasons. The overlay
// is a **child window of the console window**, not a window over the top of
// everything: the system then moves it with the console, clips it to the
// console, keeps it out of the task list and out of alt-tab, puts anything
// raised above the console above it too, and destroys it with its parent. It
// stops being a separate window and starts being part of the console, which
// is the whole point.
//
// One caveat is real and worth knowing about. The console window belongs to
// conhost.exe, a different process, so parenting to it attaches the two
// threads' input queues. That is how every overlay onto a console works and
// it is what makes the picture behave; the price is that a wedged conhost can
// wedge the thread that pumps the overlay. It is a thread of its own for
// exactly that reason, and nothing the rest of f4 does waits on it.

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetConsoleWindow        = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleScreenBuffer  = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetCurrentConsoleFontEx = kernel32.NewProc("GetCurrentConsoleFontEx")
	procGetStdHandle            = kernel32.NewProc("GetStdHandle")
	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")

	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procShowWindow       = user32.NewProc("ShowWindow")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procSetWindowRgn     = user32.NewProc("SetWindowRgn")

	procStretchDIBits     = gdi32.NewProc("StretchDIBits")
	procCreateRectRgn     = gdi32.NewProc("CreateRectRgn")
	procCombineRgn        = gdi32.NewProc("CombineRgn")
	procDeleteObject      = gdi32.NewProc("DeleteObject")
	procSetStretchBltMode = gdi32.NewProc("SetStretchBltMode")
)

const (
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipSiblings = 0x04000000

	swpNoActivate = 0x0010
	swpNoZOrder   = 0x0004
	swpShowWindow = 0x0040
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001

	swHide = 0

	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmNCHitTest   = 0x0084
	wmDestroy     = 0x0002
	wmApp         = 0x8000
	wmOverlayQuit = wmApp + 1

	htTransparent = ^uintptr(0) // HTTRANSPARENT is -1

	diRGBColors     = 0
	srcCopy         = 0x00CC0020
	rgnOr           = 2
	colorOnColor    = 3
	biRGB           = 0
	stdOutputHandle = ^uintptr(10) // STD_OUTPUT_HANDLE is -11
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type msg struct {
	HWnd    uintptr
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

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type coord struct{ X, Y int16 }

type smallRect struct{ Left, Top, Right, Bottom int16 }

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type consoleFontInfoEx struct {
	Size       uint32
	Font       uint32
	FontSize   coord
	FontFamily uint32
	FontWeight uint32
	FaceName   [32]uint16
}

// ConsoleWindow finds the console window and says how much it is worth.
func ConsoleWindow() (uintptr, Source) {
	h, _, _ := procGetConsoleWindow.Call()
	if h == 0 {
		return 0, SourceNone
	}
	visible, _, _ := procIsWindowVisible.Call(h)
	if visible == 0 {
		return h, SourceHidden
	}
	return h, SourceConsole
}

// CellSize is the pixel size of one character cell, straight from the console.
// No inference, no rounding, no escape sequence to be swallowed by whoever
// reads standard input next.
func CellSize() (int, int, bool) {
	out, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if out == 0 || out == ^uintptr(0) {
		return 0, 0, false
	}
	var info consoleFontInfoEx
	info.Size = uint32(unsafe.Sizeof(info))
	r, _, _ := procGetCurrentConsoleFontEx.Call(out, 0, uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.FontSize.X <= 0 || info.FontSize.Y <= 0 {
		return 0, 0, false
	}
	return int(info.FontSize.X), int(info.FontSize.Y), true
}

// GridSize is the console's visible size in character cells.
func GridSize() (int, int, bool) {
	out, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if out == 0 || out == ^uintptr(0) {
		return 0, 0, false
	}
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBuffer.Call(out, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0, false
	}
	w := int(info.Window.Right) - int(info.Window.Left) + 1
	h := int(info.Window.Bottom) - int(info.Window.Top) + 1
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// Overlay is one window over the console.
type Overlay struct {
	mu      sync.Mutex
	parent  uintptr
	hwnd    uintptr
	ready   chan error
	rect    Rect
	pix     []byte
	pixW    int
	pixH    int
	visible bool
	closed  bool
}

var (
	classOnce sync.Once
	classAtom uintptr
	classErr  error
	className = mustUTF16("f4ConsoleOverlay")

	// registry maps a window to its overlay, because the window procedure
	// is a C callback and cannot carry one.
	regMu sync.Mutex
	reg   = map[uintptr]*Overlay{}
)

func mustUTF16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

func registerClass() {
	classOnce.Do(func() {
		inst, _, _ := procGetModuleHandleW.Call(0)
		wc := wndClassExW{
			Size:      uint32(unsafe.Sizeof(wndClassExW{})),
			WndProc:   syscall.NewCallback(wndProc),
			Instance:  inst,
			ClassName: className,
		}
		atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			classErr = fmt.Errorf("the overlay window class could not be registered: %w", err)
			return
		}
		classAtom = atom
	})
}

func wndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmNCHitTest:
		// The mouse belongs to the console underneath. This is the same
		// rule as the empty input region on the X side, and it is what
		// keeps selecting text working while a picture is on the screen.
		return htTransparent

	case wmEraseBkgnd:
		// Painting the background would flash before the picture lands.
		return 1

	case wmPaint:
		regMu.Lock()
		o := reg[hwnd]
		regMu.Unlock()
		if o != nil {
			o.paint(hwnd)
			return 0
		}

	case wmOverlayQuit:
		procDestroyWindow.Call(hwnd)
		return 0

	case wmDestroy:
		regMu.Lock()
		delete(reg, hwnd)
		regMu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}

// New creates the overlay over the console window.
func New() (*Overlay, error) {
	parent, src := ConsoleWindow()
	if !src.Trusted() {
		return nil, fmt.Errorf("no console window to draw on: found %v", src)
	}
	registerClass()
	if classErr != nil {
		return nil, classErr
	}

	o := &Overlay{parent: parent, ready: make(chan error, 1)}
	// The window lives on a thread of its own, pumping its own messages.
	// It has to: a window belongs to the thread that created it, and this
	// one is parented across a process boundary, so a conhost that stops
	// answering must not be able to stop anything else in f4.
	go o.pump()
	if err := <-o.ready; err != nil {
		return nil, err
	}
	return o, nil
}

func (o *Overlay) pump() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	inst, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		classAtom,
		0,
		uintptr(wsChild|wsClipSiblings),
		0, 0, 1, 1,
		o.parent, 0, inst, 0,
	)
	if hwnd == 0 {
		o.ready <- fmt.Errorf("the overlay window could not be created: %w", err)
		return
	}

	regMu.Lock()
	reg[hwnd] = o
	regMu.Unlock()

	o.mu.Lock()
	o.hwnd = hwnd
	o.mu.Unlock()
	o.ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// paint blits the cached frame. It runs on the pump thread.
func (o *Overlay) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	o.mu.Lock()
	pix, w, h := o.pix, o.pixW, o.pixH
	o.mu.Unlock()
	if len(pix) == 0 || w <= 0 || h <= 0 {
		return
	}

	bi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      int32(h),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
	procSetStretchBltMode.Call(hdc, colorOnColor)
	procStretchDIBits.Call(hdc,
		0, 0, uintptr(w), uintptr(h),
		0, 0, uintptr(w), uintptr(h),
		uintptr(unsafe.Pointer(&pix[0])),
		uintptr(unsafe.Pointer(&bi)),
		diRGBColors, srcCopy)
}

// Place moves the overlay, in the console's client coordinates.
func (o *Overlay) Place(r Rect) error {
	o.mu.Lock()
	hwnd, closed := o.hwnd, o.closed
	same := r == o.rect && o.visible
	o.rect = r
	o.mu.Unlock()
	if closed || hwnd == 0 {
		return fmt.Errorf("the overlay is closed")
	}
	if r.W <= 0 || r.H <= 0 {
		o.Hide()
		return nil
	}
	if same {
		return nil
	}
	procSetWindowPos.Call(hwnd, 0,
		uintptr(int32(r.X)), uintptr(int32(r.Y)),
		uintptr(int32(r.W)), uintptr(int32(r.H)),
		swpNoActivate|swpNoZOrder|swpShowWindow)
	o.mu.Lock()
	o.visible = true
	o.mu.Unlock()
	return nil
}

// Draw hands over the pixels, RGBA, top row first, and asks for a repaint.
func (o *Overlay) Draw(pix []byte, w, h, stride int) error {
	if w <= 0 || h <= 0 || stride < w*4 || len(pix) < stride*h {
		return fmt.Errorf("a %dx%d picture needs %d bytes at stride %d, got %d",
			w, h, stride*h, stride, len(pix))
	}
	buf := make([]byte, w*h*4)
	blitInto(buf, w, h, pix, w, h, stride, 0, 0)

	o.mu.Lock()
	o.pix, o.pixW, o.pixH = buf, w, h
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("the overlay is closed")
	}
	procInvalidateRect.Call(hwnd, 0, 0)
	return nil
}

// SetBounds limits the overlay to a set of rectangles given relative to its
// own corner, so the text between them stays the console's. This is what lets
// a grid of thumbnails be one window with its captions showing through.
func (o *Overlay) SetBounds(rects []Rect) bool {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return false
	}
	if len(rects) == 0 {
		procSetWindowRgn.Call(hwnd, 0, 1)
		return true
	}

	var combined uintptr
	for _, r := range rects {
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		part, _, _ := procCreateRectRgn.Call(
			uintptr(int32(r.X)), uintptr(int32(r.Y)),
			uintptr(int32(r.X+r.W)), uintptr(int32(r.Y+r.H)))
		if part == 0 {
			continue
		}
		if combined == 0 {
			combined = part
			continue
		}
		procCombineRgn.Call(combined, combined, part, rgnOr)
		procDeleteObject.Call(part)
	}
	if combined == 0 {
		return false
	}
	// The window owns the region once it is set, so it is not deleted here.
	r, _, _ := procSetWindowRgn.Call(hwnd, combined, 1)
	if r == 0 {
		procDeleteObject.Call(combined)
		return false
	}
	return true
}

// Hide takes the picture off the screen without giving up the window.
func (o *Overlay) Hide() {
	o.mu.Lock()
	hwnd, visible := o.hwnd, o.visible
	o.visible = false
	o.rect = Rect{}
	o.mu.Unlock()
	if hwnd != 0 && visible {
		procShowWindow.Call(hwnd, swHide)
	}
}

// Visible reports whether the picture is on the screen.
func (o *Overlay) Visible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.visible
}

// ClientSize is the console window's client area in pixels.
func (o *Overlay) ClientSize() (int, int, bool) {
	o.mu.Lock()
	parent := o.parent
	o.mu.Unlock()
	var r rect
	res, _, _ := procGetClientRect.Call(parent, uintptr(unsafe.Pointer(&r)))
	if res == 0 {
		return 0, 0, false
	}
	return int(r.Right - r.Left), int(r.Bottom - r.Top), true
}

// Close destroys the window and stops its thread.
func (o *Overlay) Close() {
	o.mu.Lock()
	hwnd := o.hwnd
	if o.closed || hwnd == 0 {
		o.closed = true
		o.mu.Unlock()
		return
	}
	o.closed = true
	o.hwnd = 0
	o.mu.Unlock()
	// Destroying a window has to happen on the thread that made it.
	procPostMessageW.Call(hwnd, wmOverlayQuit, 0, 0)
}
