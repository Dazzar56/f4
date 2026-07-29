package main

import (
	"fmt"

	"github.com/mattn/go-runewidth"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// Bookmarks dialog: ten fixed rows, one per slot, reachable from
// F9 → Commands → Bookmarks. Same layout and keys as far2l's
// BookmarksMenu (bookmarks/BookmarksMenu.cpp) — the port is of the UX,
// not of the code.

// bookmarksFrame wraps a vtui.VMenu so the dialog can draw a hotkey hint
// on its bottom border without extending vtui itself, exactly the way
// userMenuFrame does it for the F2 menu.
type bookmarksFrame struct {
	*vtui.VMenu
	bottomHint string
}

func (b *bookmarksFrame) Show(scr *vtui.ScreenBuf) {
	b.VMenu.Show(scr)
	if b.bottomHint == "" {
		return
	}
	x1, _, x2, y2 := b.VMenu.GetPosition()
	vtui.NewPainter(scr).DrawTitle(x1, y2, x2, b.bottomHint, vtui.Palette[vtui.ColMenuTitle])
}

// bookmarksDialog owns the table as loaded from disk plus the menu that
// displays it. Every mutation writes straight back to file, so there is
// no "apply" step and nothing to undo on Esc.
type bookmarksDialog struct {
	pf   *PanelsFrame
	file string
	set  BookmarkSet
	menu *vtui.VMenu
}

// ShowBookmarksDialog is the entry point wired to CmBookmarks.
func ShowBookmarksDialog(pf *PanelsFrame) {
	d, err := newBookmarksDialog(pf, BookmarksFilePath())
	if err != nil {
		vtui.ShowMessage(Msg("Bookmarks.Title"),
			fmt.Sprintf(Msg("Bookmarks.LoadError"), err),
			[]string{"&Ok"})
		return
	}
	d.open()
}

// newBookmarksDialog reads the table from path. The error is returned
// rather than displayed so the caller decides how to surface it — and so
// this half can be exercised without a live UI.
func newBookmarksDialog(pf *PanelsFrame, path string) (*bookmarksDialog, error) {
	set, err := LoadBookmarks(path)
	if err != nil {
		return nil, err
	}
	return &bookmarksDialog{pf: pf, file: path, set: set}, nil
}

// open builds the menu and pushes it as a modal frame.
func (d *bookmarksDialog) open() {
	// Empty rows carry CmBookmarkEmptySlot, which is permanently disabled:
	// vtui then draws them dimmed and swallows Enter on them, which is
	// exactly the "empty slot is a no-op" behavior far2l has. No other
	// menu uses this command, so it never needs re-enabling.
	vtui.FrameManager.DisabledCommands.Disable(CmBookmarkEmptySlot)

	d.menu = vtui.NewVMenu(Msg("Bookmarks.Title"))
	d.render()

	w, h := d.size()
	x := (d.pf.lastW - w) / 2
	y := (d.pf.lastH - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	d.menu.SetPosition(x, y, x+w-1, y+h-1)

	d.menu.OnKeyDown = func(e *vtinput.InputEvent) bool {
		if !e.KeyDown {
			return false
		}
		shift := e.ControlKeyState&vtinput.ShiftPressed != 0
		ctrl := e.ControlKeyState&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed) != 0
		alt := e.ControlKeyState&(vtinput.LeftAltPressed|vtinput.RightAltPressed) != 0

		// Keys the top frame declines fall through to vtui's own global
		// handlers — F1 opens help, F9 activates the menu bar (which a
		// TypeMenu frame does not block), F12 opens the screen list.
		// None of that belongs on top of a modal dialog, so the whole
		// F-key range is swallowed, as the F2 user menu does. F10 is
		// left to vtui, which closes the menu with it.
		if !ctrl && !shift && !alt &&
			e.VirtualKeyCode >= vtinput.VK_F1 && e.VirtualKeyCode <= vtinput.VK_F12 &&
			e.VirtualKeyCode != vtinput.VK_F10 {
			return true
		}
		return false
	}

	// Enter (and a mouse click) on a populated row: vtui pops the menu on
	// its own once we return, so the navigation is posted for after the
	// frame stack has settled. Empty rows never get here — their command
	// is disabled, so vtui swallows the key first.
	d.menu.OnAction = func(uiIdx int) {
		slot := d.slotAt(uiIdx)
		if slot < 0 || d.set[slot].IsEmpty() {
			return
		}
		path := d.set[slot].Path
		pf := d.pf
		vtui.FrameManager.PostTask(func() {
			if fsp := pf.getActivePanel(); fsp != nil {
				pf.NavigateToPath(fsp, path)
			}
		})
	}

	vtui.FrameManager.Push(&bookmarksFrame{VMenu: d.menu, bottomHint: Msg("Bookmarks.BottomHint")})
}

// render rebuilds all ten rows in place, keeping the cursor where it was.
// The frame may already be on screen: vtui picks the new items up on the
// next redraw.
func (d *bookmarksDialog) render() {
	pos := d.menu.SelectPos
	d.menu.Items = nil
	d.menu.ItemCount = 0
	for i := range d.set {
		item := vtui.MenuItem{Text: d.rowText(i), UserData: i}
		if d.set[i].IsEmpty() {
			item.Command = CmBookmarkEmptySlot
		}
		d.menu.AddItem(item)
	}
	d.menu.SetSelectPos(pos)
}

// rowText formats one row: the hotkey reminder far2l prints in its own
// dialog title, the slot digit, then the path (or the empty marker).
func (d *bookmarksDialog) rowText(slot int) string {
	path := d.set[slot].Path
	if path == "" {
		path = Msg("Bookmarks.EmptySlot")
	}
	return fmt.Sprintf("%s %d   %s", Msg("Bookmarks.RowPrefix"), slot, escapeAmpersand(path))
}

// size returns the menu box dimensions: wide enough for the longest row
// and for the bottom hint, tall enough for all ten slots, clamped to the
// console — same shape of arithmetic as menuSize in user_menu_ui.go.
func (d *bookmarksDialog) size() (int, int) {
	w := 60
	for i := range d.set {
		if rw := runewidth.StringWidth(d.rowText(i)) + 4; rw > w {
			w = rw
		}
	}
	if minForHint := runewidth.StringWidth(Msg("Bookmarks.BottomHint")) + 2; w < minForHint {
		w = minForHint
	}
	if d.pf.lastW > 0 && w > d.pf.lastW-4 {
		w = d.pf.lastW - 4
	}
	if w < 24 {
		w = 24
	}

	h := len(d.set) + 2
	maxH := d.pf.lastH - 6
	if maxH < 5 {
		maxH = 5
	}
	if h > maxH {
		h = maxH
	}
	return w, h
}

// slotAt maps a menu row to its slot index, or -1 when the row is out of
// range. Rows and slots are 1:1 today; going through UserData keeps that
// an implementation detail.
func (d *bookmarksDialog) slotAt(uiPos int) int {
	if d.menu == nil || uiPos < 0 || uiPos >= len(d.menu.Items) {
		return -1
	}
	slot, ok := d.menu.Items[uiPos].UserData.(int)
	if !ok || slot < 0 || slot >= len(d.set) {
		return -1
	}
	return slot
}
