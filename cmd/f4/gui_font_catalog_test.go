package main

import (
	"reflect"
	"testing"

	"github.com/unxed/vtui"
)

func TestParseFontconfigPathsDeduplicatesAndFilters(t *testing.T) {
	got := parseFontconfigPaths("/fonts/NotoSansCJK.ttc\n/fonts/NotoSansCJK.ttc\nnot-a-font.txt\n /fonts/Mono.ttf \n")
	want := []string{"/fonts/Mono.ttf", "/fonts/NotoSansCJK.ttc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseFontconfigPaths = %#v, want %#v", got, want)
	}
}

func TestGuiFontChoicesPreserveManualValueAndCJKRecommendation(t *testing.T) {
	previous := discoverInstalledGuiFonts
	discoverInstalledGuiFonts = func(string) []string {
		return []string{"/fonts/NotoSansCJK.ttc", "/fonts/Mono.ttf", "/fonts/NotoSansCJK.ttc"}
	}
	t.Cleanup(func() { discoverInstalledGuiFonts = previous })

	got := guiFontChoices("zh", "/custom/font.otf")
	want := []string{"/custom/font.otf", "/fonts/NotoSansCJK.ttc", "/fonts/Mono.ttf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guiFontChoices = %#v, want %#v", got, want)
	}
	if shouldSuggestFontForLanguage("en", "") {
		t.Fatal("English must not trigger a CJK font recommendation")
	}
	if !shouldSuggestFontForLanguage("zh_CN", "") {
		t.Fatal("empty font must trigger a CJK font recommendation")
	}
	if shouldSuggestFontForLanguage("zh-CN", "/fonts/NotoSansCJK.ttc") {
		t.Fatal("already selected CJK font must not trigger another recommendation")
	}
	if !shouldSuggestFontForLanguage("ja", "/custom/font.otf") {
		t.Fatal("unknown manual font must trigger a CJK font recommendation")
	}
}

func TestFontconfigPatternByLanguage(t *testing.T) {
	for _, test := range []struct {
		language string
		want     string
	}{
		{language: "zh_CN", want: ":lang=zh"},
		{language: "ja-JP", want: ":lang=ja"},
		{language: "ko", want: ":lang=ko"},
		{language: "en", want: ":spacing=100"},
	} {
		if got := cjkFontconfigPattern(test.language); got != test.want {
			t.Errorf("cjkFontconfigPattern(%q) = %q, want %q", test.language, got, test.want)
		}
	}
}

func TestAppearanceSettingsFontComboRemainsEditable(t *testing.T) {
	previous := discoverInstalledGuiFonts
	discoverInstalledGuiFonts = func(string) []string { return []string{"/fonts/NotoSansCJK.ttc", "/fonts/Mono.ttf"} }
	t.Cleanup(func() { discoverInstalledGuiFonts = previous })

	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()
	oldConfig := AppConfig
	oldPath := getUserConfigIniPath
	AppConfig.GuiFont = "/custom/font.otf"
	AppConfig.Language = "zh"
	getUserConfigIniPath = func() string { return t.TempDir() + "/settings.ini" }
	t.Cleanup(func() {
		AppConfig = oldConfig
		getUserConfigIniPath = oldPath
	})

	pf := NewPanelsFrame()
	t.Cleanup(pf.Close)
	pf.ResizeConsole(80, 25)
	actionAppearanceSettings(pf)
	top := vtui.FrameManager.GetTopFrame().(vtui.Container)

	var fontCombo *vtui.ComboBox
	for _, child := range top.GetChildren() {
		combo, ok := child.(*vtui.ComboBox)
		if !ok || len(combo.Menu.Items) == 0 {
			continue
		}
		if combo.Menu.Items[0].Text == "/custom/font.otf" {
			fontCombo = combo
			break
		}
	}
	if fontCombo == nil {
		t.Fatal("font catalog combobox not found")
	}
	if fontCombo.DropdownOnly {
		t.Fatal("font catalog combobox must preserve manual entry")
	}
	fontCombo.Edit.SetText("/manually/entered/font.ttf")
	clickDialogButton(t, top, "Ok")
	if AppConfig.GuiFont != "/manually/entered/font.ttf" {
		t.Fatalf("manual font path = %q", AppConfig.GuiFont)
	}
}
