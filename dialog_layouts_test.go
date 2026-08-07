package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestAllDialogs_LayoutValidation(t *testing.T) {
	vtui.SetDefaultPalette()

	// Warm up the Colorer scheme cache so subsequent dialog openings don't pay the
	// disk scan cost for every test case.
	_ = ListColorerSchemes()

	// Actions that are destructive, async, or mutate global state without a dialog.
	skipActions := map[string]bool{
		"app.quit":                         true,
		"panel.systemexplorer":             true,
		"app.togglewindowsize":             true,
		"panel.rescan":                     true,
		"panel.swap":                       true,
		"panel.toggle":                     true,
		"panel.toggleleftpanel":            true,
		"panel.togglerightpanel":           true,
		"panel.togglepassivepanel":         true,
		"panel.splitleft":                  true,
		"panel.splitright":                 true,
		"panel.splitup":                    true,
		"panel.splitdown":                  true,
		"panel.splitactiveup":              true,
		"panel.splitactivedown":            true,
		"panel.splitreset":                 true,
		"panel.viewbrief":                  true,
		"panel.viewmedium":                 true,
		"panel.viewdetailed":               true,
		"panel.viewwide":                   true,
		"panel.sortbyname":                 true,
		"panel.sortbyext":                  true,
		"panel.sortbytime":                 true,
		"panel.sortbysize":                 true,
		"panel.sortunsorted":               true,
		"panel.togglekeybar":               true,
		"panel.toggleinfobytes":            true,
		"panel.togglehidden":               true,
		"panel.historyback":                true,
		"panel.historyforward":             true,
		"file.view":                        true,
		"file.edit":                        true,
		"file.new":                         true,
		"file.attributes":                  true,
		"file.findduplicates":              true,
		"terminal.viewlog":                 true,
		"terminal.editlog":                 true,
		"editor.switchtoviewer":            true,
		"viewer.switchtoeditor":            true,
		"editor.codepagenext":              true,
		"viewer.codepagenext":              true,
		"editor.save":                      true,
		"editor.undo":                      true,
		"editor.redo":                      true,
		"editor.copy":                      true,
		"editor.cut":                       true,
		"editor.paste":                     true,
		"editor.selectall":                 true,
		"editor.deleteline":                true,
		"editor.toggleovertype":            true,
		"editor.searchnext":                true,
		"editor.wordwrap":                  true,
		"editor.showwhitespaces":           true,
		"editor.insertleftpanelpath":       true,
		"editor.insertrightpanelpath":      true,
		"editor.insertactivepanelfilename": true,
		"editor.deletespacersforward":      true,
		"viewer.wrapmode":                  true,
		"viewer.hexmode":                   true,
		"app.savesettings":                 true,
		"panel.copypath":                   true,
		"panel.copyname":                   true,
		"panel.copyselectednames":          true,
		"panel.copyselectedpaths":          true,
		"panel.copyselectedrealpaths":      true,
		"panel.invertselection":            true,
		"panel.restoreselection":           true,
		"app.screengrab":                   true,
		"app.plugring":                     true,
		"panel.leftdrivemenu":              true,
		"panel.rightdrivemenu":             true,
		"panel.enterdirectory":             true,
		"panel.insertfilename":             true,
		"panel.insertleftpath":             true,
		"panel.insertrightpath":            true,
		"debug.dummyoperation":             true,
		"panel.infopanel":                  true,
		"panel.quickview":                  true,
	}

	// Load all language packs so the validator can assert layout against all translations dynamically
	packs := LoadAllLanguagePacks()

	for _, act := range GetActions() {
		name := act.Name
		if skipActions[strings.ToLower(name)] {
			continue
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subTmp := t.TempDir()

			// Override config path for this subtest
			oldGetConfig := getUserConfigIniPath
			oldConfig := AppConfig
			defer func() {
				getUserConfigIniPath = oldGetConfig
				AppConfig = oldConfig
			}()
			getUserConfigIniPath = func() string {
				return filepath.Join(subTmp, "settings.ini")
			}

			// Create a dummy file in the sub-temp directory
			srcFile := filepath.Join(subTmp, "test.txt")
			if err := os.WriteFile(srcFile, []byte("dummy content"), 0644); err != nil {
				t.Fatal(err)
			}

			oldHotkeys := GlobalHotkeysMgr
			oldMacro := MacroMgr
			defer func() {
				GlobalHotkeysMgr = oldHotkeys
				MacroMgr = oldMacro
			}()
			GlobalHotkeysMgr = NewHotkeyManager(filepath.Join(subTmp, "hotkeys.ini"))
			MacroMgr = NewMacroManager(filepath.Join(subTmp, "key_macros.ini"))

			rules := vtui.DefaultLayoutRules
			rules.MaxWidth = 120

			errs := vtui.ValidateLayoutInLanguagesWithRules(packs, rules, func() vtui.Container {
				scr := vtui.NewSilentScreenBuf()
				scr.AllocBuf(120, 60)
				vtui.FrameManager.Init(scr)

				localVFS := vfs.NewOSVFS(subTmp)
				_ = localVFS.SetPath(subTmp)
				pf := NewPanelsFrame()
				pf.panels[0] = NewFileSystemPanel(0, 0, 40, 20, localVFS)
				pf.panels[1] = NewFileSystemPanel(40, 0, 40, 20, localVFS.Clone())
				pf.ResizeConsole(120, 60)
				vtui.FrameManager.Push(pf)

				if strings.HasPrefix(name, "Editor.") {
					showEditor(pf, localVFS, srcFile, &vfs.MemoryReadAtCloser{Data: []byte("dummy")})
				} else if strings.HasPrefix(name, "Viewer.") {
					vv, err := NewViewerView(context.Background(), localVFS, srcFile)
					if err == nil {
						showViewer(pf, vv, srcFile)
					}
				}

				initialCount := len(vtui.FrameManager.Screens[vtui.FrameManager.ActiveIdx].Frames)
				act.Handler()
				frames := vtui.FrameManager.Screens[vtui.FrameManager.ActiveIdx].Frames
				if len(frames) <= initialCount {
					return nil
				}
				topFrame := frames[len(frames)-1]
				container, ok := topFrame.(vtui.Container)
				if !ok {
					return nil
				}
				return container
			})

			// Clean up frames
			for _, s := range vtui.FrameManager.Screens {
				for _, f := range s.Frames {
					f.Close()
				}
			}

			if len(errs) > 0 {
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				t.Errorf("Layout validation failed for Action %s:\n%s", name, strings.Join(msgs, "\n"))
			}
		})
	}
}
