package main

import (
	"testing"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/vtui"
)

// useStubHistory swaps in an empty in-memory provider for the duration of a
// test so nothing touches the real history.json.
func useStubHistory(t *testing.T) stubHistoryProvider {
	t.Helper()
	store := stubHistoryProvider{}
	previous := vtui.GlobalHistoryProvider
	vtui.GlobalHistoryProvider = store
	t.Cleanup(func() { vtui.GlobalHistoryProvider = previous })
	return store
}

// findHistoryEdit returns the dialog's history backed field for the given
// bucket, so the tests do not depend on the order items were added in.
func findHistoryEdit(t *testing.T, dlg vtui.Container, historyID string) *vtui.Edit {
	t.Helper()
	for _, child := range dlg.GetChildren() {
		if edit, ok := child.(*vtui.Edit); ok && edit.HistoryID == historyID {
			return edit
		}
	}
	t.Fatalf("no edit field bound to history %q", historyID)
	return nil
}

func TestAttachHistory_LoadsExistingEntries(t *testing.T) {
	store := useStubHistory(t)
	store["SearchText"] = []string{"needle", "haystack"}

	edit := vtui.NewEdit(0, 0, 20, "current")
	attachHistory(edit, searchTextHistoryID)

	if !edit.ShowHistoryButton {
		t.Error("history field should show the drop-down marker")
	}
	if len(edit.History) != 2 || edit.History[0] != "needle" {
		t.Errorf("history not loaded eagerly: %v", edit.History)
	}
	// Ctrl+E walks the local slice, so it must work without opening the menu.
	edit.HistoryUp()
	if got := edit.GetText(); got != "needle" {
		t.Errorf("HistoryUp gave %q, want %q", got, "needle")
	}
}

func TestAttachHistoryUseLast_OnlyFillsEmptyField(t *testing.T) {
	store := useStubHistory(t)
	store["SearchText"] = []string{"remembered"}

	empty := attachHistoryUseLast(vtui.NewEdit(0, 0, 20, ""), searchTextHistoryID)
	if got := empty.GetText(); got != "remembered" {
		t.Errorf("empty field: got %q, want %q", got, "remembered")
	}
	// Pre-filled text must be selected, so typing replaces rather than appends.
	empty.InsertString("x")
	if got := empty.GetText(); got != "x" {
		t.Errorf("typing over the pre-filled entry gave %q, want %q", got, "x")
	}

	filled := attachHistoryUseLast(vtui.NewEdit(0, 0, 20, "typed"), searchTextHistoryID)
	if got := filled.GetText(); got != "typed" {
		t.Errorf("non-empty field was overwritten: got %q", got)
	}
}

func TestCommitHistory_IgnoresUnboundAndEmpty(t *testing.T) {
	store := useStubHistory(t)

	commitHistory(nil, "value")
	commitHistory(vtui.NewEdit(0, 0, 20, ""), "no history id")
	commitHistory(attachHistory(vtui.NewEdit(0, 0, 20, ""), searchTextHistoryID), "")

	if len(store) != 0 {
		t.Errorf("nothing should have been stored, got %v", store)
	}
}

func TestCommitHistory_MovesRepeatToFront(t *testing.T) {
	store := useStubHistory(t)
	store["SearchText"] = []string{"older", "repeat"}

	edit := attachHistory(vtui.NewEdit(0, 0, 20, ""), searchTextHistoryID)
	commitHistory(edit, "repeat")

	want := []string{"repeat", "older"}
	got := store["SearchText"]
	if len(got) != len(want) {
		t.Fatalf("history is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history is %v, want %v", got, want)
		}
	}
}

func TestEditorSearchDialog_RemembersPattern(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	store := useStubHistory(t)
	store["SearchText"] = []string{"from history"}

	oldSearch := LastEditorSearch
	t.Cleanup(func() { LastEditorSearch = oldSearch })
	LastEditorSearch = ""

	ev := NewEditorView(piecetable.New([]byte("alpha beta\n")), nil, "test.txt")
	ev.showSearchDialog()
	dlg := vtui.FrameManager.GetTopFrame().(vtui.Container)
	defer vtui.FrameManager.Pop()

	edit := findHistoryEdit(t, dlg, searchTextHistoryID)
	if got := edit.GetText(); got != "from history" {
		t.Errorf("empty search field was not pre-filled: got %q", got)
	}

	edit.SetText("beta")
	commitHistory(edit, edit.GetText())
	if got := store["SearchText"]; len(got) == 0 || got[0] != "beta" {
		t.Errorf("accepted pattern not stored: %v", got)
	}
}

func TestEditorReplaceDialog_UsesSeparateBuckets(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	store := useStubHistory(t)
	store["ReplaceText"] = []string{"stale replacement"}

	oldSearch, oldReplace := LastEditorSearch, LastEditorReplace
	t.Cleanup(func() { LastEditorSearch, LastEditorReplace = oldSearch, oldReplace })
	LastEditorSearch, LastEditorReplace = "", ""

	ev := NewEditorView(piecetable.New([]byte("alpha beta\n")), nil, "test.txt")
	ev.showReplaceDialog()
	dlg := vtui.FrameManager.GetTopFrame().(vtui.Container)
	defer vtui.FrameManager.Pop()

	replace := findHistoryEdit(t, dlg, replaceTextHistoryID)
	if got := replace.GetText(); got != "" {
		t.Errorf("replacement field must not be pre-filled from history, got %q", got)
	}
	if len(replace.History) != 1 {
		t.Errorf("replacement field should still offer its history: %v", replace.History)
	}
	// The search field is a different bucket and must not pick up replacements.
	pattern := findHistoryEdit(t, dlg, searchTextHistoryID)
	if len(pattern.History) != 0 {
		t.Errorf("search field leaked replacement history: %v", pattern.History)
	}
}

func TestFindFileDialog_SharesSearchTextBucket(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	store := useStubHistory(t)
	store["SearchText"] = []string{"typed in the editor"}
	store["Masks"] = []string{"*.go"}

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	actionFindFile(pf)
	dlg := vtui.FrameManager.GetTopFrame().(vtui.Container)
	defer vtui.FrameManager.Pop()

	text := findHistoryEdit(t, dlg, searchTextHistoryID)
	if len(text.History) != 1 || text.History[0] != "typed in the editor" {
		t.Errorf("containing-text field does not share the editor bucket: %v", text.History)
	}
	mask := findHistoryEdit(t, dlg, fileMasksHistoryID)
	if len(mask.History) != 1 || mask.History[0] != "*.go" {
		t.Errorf("mask field history not loaded: %v", mask.History)
	}
	// The mask defaults to "*", which must survive DIF_HISTORY wiring.
	if got := mask.GetText(); got != LastFindFileMask {
		t.Errorf("mask field is %q, want %q", got, LastFindFileMask)
	}
}

func TestViewerSearchDialog_AttachesHistory(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	store := useStubHistory(t)
	store["SearchText"] = []string{"previous search"}

	vv := &ViewerView{}
	actionViewerSearchDirection(vv, false)
	dlg := vtui.FrameManager.GetTopFrame().(vtui.Container)
	defer vtui.FrameManager.Pop()

	edit := findHistoryEdit(t, dlg, searchTextHistoryID)
	if got := edit.GetText(); got != "previous search" {
		t.Errorf("viewer search field not pre-filled: got %q", got)
	}
	if len(edit.History) != 1 {
		t.Errorf("viewer search history not loaded: %v", edit.History)
	}
}
