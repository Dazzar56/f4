package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/unxed/vtui"
)

// commandPaletteWorkspaceEntries exposes every live workspace as a separate
// command. The entry captures the stable workspace number, not its current
// slice index: tabs can be reordered while the palette is open.
func commandPaletteWorkspaceEntries() []commandPaletteEntry {
	if vtui.FrameManager == nil || len(vtui.FrameManager.Screens) == 0 {
		return nil
	}

	category := Msg("CommandPalette.CategoryWorkspace")
	aliases := commandPaletteTranslations(
		"CommandPalette.CategoryWorkspace",
		"CommandPalette.Workspace.Switch",
		"CommandPalette.Workspace.Switch.Desc",
		"AppearanceSettings.WorkspaceTabs",
		"AppearanceSettings.RestoreWorkspaceTabs",
	)
	entries := make([]commandPaletteEntry, 0, len(vtui.FrameManager.Screens))
	for index, screen := range vtui.FrameManager.Screens {
		if screen == nil || screen.Number < 1 {
			continue
		}
		number := screen.Number
		info := screen.GetMenuInfo()
		primary := strings.TrimSpace(info.Primary)
		if primary == "" {
			primary = strings.TrimSpace(screen.GetTabTitle())
		}
		if primary == "" {
			primary = "Workspace"
		}
		secondary := strings.TrimSpace(info.Secondary)
		description := fmt.Sprintf(Msg("CommandPalette.Workspace.Switch.Desc"), number)
		if secondary != "" {
			description += ": " + secondary
		}

		searchFields := []string{
			strconv.Itoa(number),
			primary,
			secondary,
			strings.TrimSpace(screen.GetTabTitle()),
			strings.TrimSpace(screen.GetTitle()),
			strings.TrimSpace(info.Icon),
			"workspace",
			"screen",
		}
		searchFields = append(searchFields, aliases...)
		entries = append(entries, commandPaletteEntry{
			Key:                fmt.Sprintf("workspace:activate:%d", number),
			Label:              fmt.Sprintf(Msg("CommandPalette.Workspace.Switch"), number, primary),
			EnglishLabel:       fmt.Sprintf("Switch to workspace %d: %s", number, primary),
			Description:        description,
			EnglishDescription: fmt.Sprintf("Activate workspace %d", number),
			ID:                 fmt.Sprintf("Workspace.Activate.%d", number),
			Category:           category,
			Shortcut:           strings.Join(workspaceNumberShortcuts(number), ", "),
			SearchFields:       searchFields,
			Checked:            index == vtui.FrameManager.ActiveIdx,
			run:                func() bool { return actionActivateWorkspaceNumber(number) },
		})
	}
	return entries
}

func workspaceNumberShortcuts(number int) []string {
	if vtui.FrameManager == nil || !vtui.FrameManager.WorkspaceAltNumberSwitch || number < 1 || number > 9 {
		return nil
	}
	shortcuts := []string{FormatKeyForUI(fmt.Sprintf("Alt%d", number))}
	if vtui.FrameManager.WorkspaceTabMode == vtui.WorkspaceTabsOnCtrl {
		shortcuts = append(shortcuts, FormatKeyForUI(fmt.Sprintf("CtrlAlt%d", number)))
	}
	return mergeCommandPaletteShortcuts(shortcuts)
}
