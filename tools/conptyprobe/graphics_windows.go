//go:build windows

// The console-image and terminal-graphics probe. Section 1 already records the
// console window's class, visibility, WS_CLIPCHILDREN, WS_EX_LAYERED, owner and
// DA1; that is enough to *decide* between the overlay path (classic conhost)
// and the sixel path (Windows Terminal). This section measures the things the
// overlay itself and the sixel encoder actually depend on, which section 1 does
// not touch:
//
//   - the console cell in pixels (GetCurrentConsoleFontEx.dwFontSize) -- the
//     exact input to internal/wincon.CellSize(), so a field run says whether
//     the overlay can place pixels at all and at what scale;
//   - whether the redesigned overlay's *mechanism* works on this build: a
//     top-level WS_EX_LAYERED|WS_EX_TRANSPARENT window, positioned and filled
//     in one UpdateLayeredWindow call with premultiplied BGRA, then lifted just
//     above the console with SetWindowPos (WINCON_805_HANDOVER.md step 3). The
//     probe creates one such window off-screen, drives it, and tears it down;
//   - the z-order chain directly above the console (handover Q6): what, if
//     anything, already sits between the console and the top, which is what an
//     "insert above the console" rule has to cope with;
//   - the sixel capability report XTSMGRAPHICS is meant to give. Section 1
//     asked it once and got silence under *both* conhost and Windows Terminal;
//     this retries several documented request forms and a longer window, so a
//     WT field run finally says whether the color-register and geometry numbers
//     f4's encoder relies on (docs/IMAGES_PLAN.md sections 5-6) can be read
//     from the terminal or must stay hard-coded.
//
// Nothing here is destructive: the only window created belongs to the probe and
// is destroyed before the section returns; the VT queries only read replies.
package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procGetCurrentConsoleFontEx = kernel32.NewProc("GetCurrentConsoleFontEx")
	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procUnregisterClassW        = user32.NewProc("UnregisterClassW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procShowWindow              = user32.NewProc("ShowWindow")
	procSetWindowPos            = user32.NewProc("SetWindowPos")
	procUpdateLayeredWindow     = user32.NewProc("UpdateLayeredWindow")
	procGetDC                   = user32.NewProc("GetDC")
	procReleaseDC               = user32.NewProc("ReleaseDC")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

const (
	swpNoActivate  = 0x0010
	swpNoMove      = 0x0002
	swpNoSize      = 0x0001
	swShowNoActive = 4
	swHide         = 0
	biRGB          = 0
	dibRGBColors   = 0
	acSrcOver      = 0x00
	acSrcAlpha     = 0x01
	ulwAlpha       = 0x00000002
)

type consoleFontInfoEx struct {
	Size       uint32
	Font       uint32
	FontSize   coord
	FontFamily uint32
	FontWeight uint32
	FaceName   [32]uint16
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

type point struct{ X, Y int32 }
type size struct{ Cx, Cy int32 }

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
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

func probeConsoleGraphics(w *writer) {
	w.section("1a. Console images and terminal graphics (issue #805, sixel)")
	w.printf("What the overlay and the sixel encoder depend on, beyond the host\n")
	w.printf("identification section above. Nothing here is destructive.\n")

	describeConsoleCell(w)
	probeOverlayMechanism(w)
	probeZOrderAboveConsole(w)
	probeSixelCapabilities(w)
}

// describeConsoleCell reports the pixel size of one character cell -- the exact
// number internal/wincon.CellSize() feeds the overlay. Under Windows Terminal
// GetConsoleWindow is a 0x0 pseudo window and this call may return a default or
// fail, which is itself worth recording: the overlay is not used there anyway.
func describeConsoleCell(w *writer) {
	w.sub("console cell size (overlay geometry input)")
	out := getStdHandle(stdOutputHandle)
	var info consoleFontInfoEx
	info.Size = uint32(unsafe.Sizeof(info))
	r, _, err := procGetCurrentConsoleFontEx.Call(uintptr(out), 0, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		w.printf("GetCurrentConsoleFontEx failed: %v\n", err)
		w.summary("gfx.cell", "unreadable")
		return
	}
	face := syscall.UTF16ToString(info.FaceName[:])
	w.printf("cell = %dx%d px, font index %d, weight %d, face %q\n",
		info.FontSize.X, info.FontSize.Y, info.Font, info.FontWeight, face)
	w.summary("gfx.cell", fmt.Sprintf("%dx%d", info.FontSize.X, info.FontSize.Y))
	w.summary("gfx.cell.face", emptyAs(face, "(none)"))
	if info.FontSize.X <= 0 || info.FontSize.Y <= 0 {
		w.printf("note: a non-positive cell means the overlay cannot place pixels; expected under a pseudo console.\n")
	}
}

// probeOverlayMechanism creates the exact window the redesigned overlay uses --
// a top-level WS_EX_LAYERED|WS_EX_TRANSPARENT|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE
// popup with no parent and no owner -- fills it in one UpdateLayeredWindow call
// with premultiplied BGRA, and lifts it just above the console with
// SetWindowPos. It answers, per build, the three things step 3 of the handover
// asserts but has not measured on a machine: that such a window is accepted,
// that UpdateLayeredWindow with a premultiplied 32bpp DIB succeeds, and that it
// can be placed directly above the console window. The window is 8x8, drawn at
// an off-screen position, shown without activation, and destroyed immediately.
func probeOverlayMechanism(w *writer) {
	w.sub("overlay mechanism: layered top-level window (handover step 3)")

	inst, _, _ := procGetModuleHandleW.Call(0)
	className := mustUTF16Ptr("f4probeOverlayProbe")
	def := procDefWindowProcW.Addr()
	wc := wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		WndProc:   def,
		Instance:  inst,
		ClassName: className,
	}
	atom, _, regErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		w.printf("RegisterClassExW failed: %v (cannot test the overlay window)\n", regErr)
		w.summary("gfx.overlay.window_created", "no (class registration failed)")
		return
	}
	defer procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), inst)

	const w0, h0 = 8, 8
	// Off-screen so a field run never flashes anything on the user's desktop.
	const offX, offY int32 = -20000, -20000
	coordArg := func(v int32) uintptr { return uintptr(uint32(v)) }
	hwnd, _, createErr := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTransparent|wsExToolWindow|wsExNoActivate),
		atom, 0,
		uintptr(wsPopup),
		coordArg(offX), coordArg(offY), w0, h0,
		0, 0, inst, 0,
	)
	if hwnd == 0 {
		w.printf("CreateWindowExW (layered top-level) failed: %v\n", createErr)
		w.summary("gfx.overlay.window_created", "no")
		return
	}
	w.printf("created a top-level WS_EX_LAYERED|WS_EX_TRANSPARENT|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE popup, no parent/owner\n")
	w.summary("gfx.overlay.window_created", "yes")
	defer procDestroyWindow.Call(hwnd)

	// Build a premultiplied BGRA top-down DIB and push it in one call.
	screenDC, _, _ := procGetDC.Call(0)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	bih := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    w0,
		Height:   -h0, // top-down
		Planes:   1,
		BitCount: 32,
	}
	var bits unsafe.Pointer
	dib, _, dibErr := procCreateDIBSection.Call(
		memDC, uintptr(unsafe.Pointer(&bih)), dibRGBColors,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	ulwOK := false
	if dib == 0 || bits == nil {
		w.printf("CreateDIBSection failed: %v\n", dibErr)
		w.summary("gfx.overlay.update_layered_window", "no (DIB creation failed)")
	} else {
		// Fill with a semi-transparent pixel, premultiplied (b*a/255, ...).
		px := (*[w0 * h0 * 4]byte)(bits)
		const a = 128
		for i := 0; i < w0*h0; i++ {
			px[i*4+0] = byte(0 * a / 255)   // B
			px[i*4+1] = byte(128 * a / 255) // G
			px[i*4+2] = byte(255 * a / 255) // R
			px[i*4+3] = a                   // A
		}
		old, _, _ := procSelectObject.Call(memDC, dib)
		pt := point{X: offX, Y: offY}
		sz := size{Cx: w0, Cy: h0}
		src := point{X: 0, Y: 0}
		bf := blendFunction{BlendOp: acSrcOver, SourceConstantAlpha: 255, AlphaFormat: acSrcAlpha}
		r, _, ulwErr := procUpdateLayeredWindow.Call(
			hwnd, screenDC,
			uintptr(unsafe.Pointer(&pt)), uintptr(unsafe.Pointer(&sz)),
			memDC, uintptr(unsafe.Pointer(&src)),
			0, uintptr(unsafe.Pointer(&bf)), ulwAlpha)
		ulwOK = r != 0
		if ulwOK {
			w.printf("UpdateLayeredWindow with a premultiplied 32bpp DIB: ok\n")
		} else {
			w.printf("UpdateLayeredWindow failed: %v\n", ulwErr)
		}
		w.summary("gfx.overlay.update_layered_window", yesno(ulwOK))
		procSelectObject.Call(memDC, old)
		procDeleteObject.Call(dib)
	}
	procDeleteDC.Call(memDC)
	procReleaseDC.Call(0, screenDC)

	if ulwOK {
		procShowWindow.Call(hwnd, swShowNoActive)
	}

	// Place it directly above the console window, the way the tracking timer
	// would. Success here is what makes the "insert above the console" z-order
	// rule viable on this build.
	console := consoleWindow()
	if console == 0 {
		w.printf("no console window to place the overlay above (expected under a pseudo console)\n")
		w.summary("gfx.overlay.placed_above_console", "n/a (no console window)")
	} else {
		r, _, posErr := procSetWindowPos.Call(hwnd, console, 0, 0, 0, 0,
			uintptr(swpNoMove|swpNoSize|swpNoActivate))
		if r != 0 {
			prev, _, _ := procGetWindow.Call(console, gwHwndPrev)
			w.printf("SetWindowPos above the console: ok; console's GW_HWNDPREV is now %#x (our hwnd %#x)\n", prev, hwnd)
			w.summary("gfx.overlay.placed_above_console", yesno(prev == hwnd))
		} else {
			w.printf("SetWindowPos above the console failed: %v\n", posErr)
			w.summary("gfx.overlay.placed_above_console", "no")
		}
	}
	procShowWindow.Call(hwnd, swHide)
}

// probeZOrderAboveConsole records the window chain from the console upward
// (GW_HWNDPREV walks toward the top of the z-order). Handover Q6 asks whether
// "insert just above the console" is enough or whether the overlay must instead
// hide when the console is not foreground; the answer depends on what normally
// sits above the console, which this lists.
func probeZOrderAboveConsole(w *writer) {
	w.sub("z-order above the console (handover Q6)")
	console := consoleWindow()
	if console == 0 {
		w.printf("no console window (pseudo console); z-order rule does not apply here\n")
		w.summary("gfx.zorder_above_console", "n/a (no console window)")
		return
	}
	fg, _, _ := procGetForegroundWindow.Call()
	w.printf("console hwnd=%#x foreground=%#x console_is_foreground=%v\n",
		console, fg, yesno(fg == console))
	var chain []string
	cur := console
	for i := 0; i < 6; i++ {
		prev, _, _ := procGetWindow.Call(cur, gwHwndPrev)
		if prev == 0 {
			chain = append(chain, "TOP")
			break
		}
		ex := windowLong(prev, gwlExStyle)
		chain = append(chain, fmt.Sprintf("%#x[%q topmost=%v]",
			prev, className(prev), yesno(ex&wsExTopmost != 0)))
		cur = prev
	}
	above := "(nothing -- console is already at the top)"
	if len(chain) > 0 {
		above = joinStrings(chain, " -> ")
	}
	w.printf("above the console: %s\n", above)
	w.summary("gfx.zorder_above_console", above)
}

// probeSixelCapabilities retries XTSMGRAPHICS. Section 1 asked the color-register
// and max-geometry forms once with a 600ms window and got silence from both
// conhost (expected: no sixel) and Windows Terminal (not expected: it advertises
// sixel in DA1). f4's encoder currently hard-codes the register count from
// "what Windows Terminal reports" (docs/IMAGES_PLAN.md section 6); this says
// whether it can be read at all, trying every documented request id and a longer
// timeout so a WT field run is conclusive either way.
func probeSixelCapabilities(w *writer) {
	w.sub("sixel/graphics capability report (XTSMGRAPHICS retry)")
	da1, _ := queryTerminal("\x1b[c", 'c', 600*time.Millisecond)
	sixelAdvertised := da1HasSixel(da1)
	w.printf("DA1 = %s (sixel advertised: %v)\n", emptyAs(Escape([]byte(da1)), "(no answer)"), yesno(sixelAdvertised))

	type gq struct {
		name, key, seq string
	}
	// Pi;Pa;Pv S -- Pi=1 color registers, Pi=2 cell geometry; Pa=1 read.
	// The request forms terminals actually implement vary, so try the common
	// ones rather than the single pair section 1 used.
	queries := []gq{
		{"color registers (CSI ? 1 ; 1 ; 0 S)", "gfx.xtsm.color_registers", "\x1b[?1;1;0S"},
		{"color registers (CSI ? 1 ; 1 S)", "gfx.xtsm.color_registers_alt", "\x1b[?1;1S"},
		{"cell geometry (CSI ? 2 ; 1 ; 0 S)", "gfx.xtsm.cell_geometry", "\x1b[?2;1;0S"},
		{"cell geometry (CSI ? 2 ; 1 S)", "gfx.xtsm.cell_geometry_alt", "\x1b[?2;1S"},
	}
	anyAnswer := false
	for _, q := range queries {
		ans, err := queryTerminal(q.seq, 'S', 1200*time.Millisecond)
		if err != nil {
			w.printf("%-34s -> %v\n", q.name, err)
			w.summary(q.key, err.Error())
			continue
		}
		anyAnswer = true
		esc := Escape([]byte(ans))
		w.printf("%-34s -> %s\n", q.name, esc)
		w.summary(q.key, esc)
	}
	switch {
	case !sixelAdvertised:
		w.printf("conclusion: this host does not advertise sixel; XTSMGRAPHICS silence is expected, the overlay path applies.\n")
		w.summary("gfx.xtsm.conclusion", "no sixel host; overlay path")
	case anyAnswer:
		w.printf("conclusion: sixel host answered XTSMGRAPHICS; the reported numbers can drive the encoder instead of a constant.\n")
		w.summary("gfx.xtsm.conclusion", "answered; numbers readable")
	default:
		w.printf("conclusion: sixel is advertised in DA1 but XTSMGRAPHICS stays silent; the register/geometry constants in the encoder cannot be replaced by a live query on this host.\n")
		w.summary("gfx.xtsm.conclusion", "advertised but silent; keep constants")
	}
}

func mustUTF16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
