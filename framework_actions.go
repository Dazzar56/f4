package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/vtui"
)

// actionContextHelp resolves the same contextual topic as FrameManager's F1
// fallback. It deliberately does not synthesize another F1 event: frames may
// route synthesized keys back through configured hotkeys, where a user binding
// of F1 to App.Help would recursively invoke this action.
func actionContextHelp() bool {
	if vtui.FrameManager == nil {
		return false
	}
	top := vtui.FrameManager.GetTopFrame()
	if top == nil {
		return false
	}

	topic := top.GetHelp()
	if container, ok := top.(vtui.FocusContainer); ok {
		if focused := container.GetFocusedItem(); focused != nil && focused.GetHelp() != "" {
			topic = focused.GetHelp()
		}
	}
	if topic == "" {
		topic = "Contents"
	}
	if vtui.GlobalHelpEngine == nil {
		return false
	}
	vtui.FrameManager.Push(vtui.NewHelpView(vtui.GlobalHelpEngine, topic))
	return true
}

// actionActivateMainMenu implements the same eligibility rules as the native
// F9 fallback. The palette executes it only after its own dialog has gone away,
// so GetTopFrame refers to the screen the user was working in.
func actionActivateMainMenu() bool {
	if vtui.FrameManager == nil {
		return false
	}
	top := vtui.FrameManager.GetTopFrame()
	menu := vtui.FrameManager.GetActiveMenuBar()
	if top == nil || menu == nil {
		return false
	}
	if menu.Active {
		return true
	}
	canActivate := !top.IsModal() || top.GetType() == vtui.TypeMenu || top.GetMenuBar() != nil
	if !canActivate {
		return false
	}

	menu.Active = true
	if len(menu.Items) > 0 {
		if menu.SelectPos < 0 || menu.SelectPos >= len(menu.Items) {
			menu.SelectPos = 0
		}
		menu.ActivateSubMenu(menu.SelectPos)
	}
	vtui.FrameManager.Redraw()
	return true
}

func multipleWorkspacesAvailable() bool {
	return vtui.FrameManager != nil && len(vtui.FrameManager.Screens) > 1
}

func actionWorkspaceNew() bool {
	if vtui.FrameManager == nil {
		return false
	}
	if vtui.FrameManager.HandleSemanticAction(map[string]any{
		"action": "workspace.new",
		"target": "workspace-new",
	}) {
		return true
	}

	// Full-screen editor, viewer, image and queue workspaces do not keep the
	// PanelsFrame in their own frame stack, so vtui's active-stack CmResize
	// broadcast cannot reach the frame that knows how to clone panels. Reuse
	// the same MRU-aware resolver used by the rest of f4 and ask that concrete
	// panels frame to perform its ordinary, well-tested fork operation.
	panels := findPanelsFrameAnyScreen()
	return panels != nil && panels.HandleCommand(vtui.CmResize, "fork")
}

func activeWorkspaceNumber() (int, bool) {
	if vtui.FrameManager == nil {
		return 0, false
	}
	index := vtui.FrameManager.ActiveIdx
	if index < 0 || index >= len(vtui.FrameManager.Screens) {
		return 0, false
	}
	return vtui.FrameManager.Screens[index].Number, true
}

func workspaceSemanticTarget(number int) string {
	return fmt.Sprintf("workspace-tab-%d", number)
}

func actionActivateWorkspaceNumber(number int) bool {
	if vtui.FrameManager == nil || number < 1 {
		return false
	}
	// Resolve the stable display number at execution time. Workspace order may
	// change while a palette is open; a captured slice index could then activate
	// the wrong tab.
	for _, screen := range vtui.FrameManager.Screens {
		if screen.Number != number {
			continue
		}
		return vtui.FrameManager.HandleSemanticAction(map[string]any{
			"action": "workspace.activate",
			"target": workspaceSemanticTarget(number),
		})
	}
	return false
}

func actionWorkspaceClose() bool {
	if vtui.FrameManager == nil {
		return false
	}
	// QueueFrame deliberately owns close attempts while work is active. The
	// semantic workspace API closes screens directly and would otherwise bypass
	// that veto when Workspace.Close is selected from the palette.
	if queue, ok := vtui.FrameManager.GetTopFrame().(*QueueFrame); ok {
		return actionCloseQueueWorkspace(queue)
	}
	return actionWorkspaceCloseSemantic()
}

func actionWorkspaceCloseSemantic() bool {
	number, ok := activeWorkspaceNumber()
	if !ok {
		return false
	}
	return vtui.FrameManager.HandleSemanticAction(map[string]any{
		"action": "workspace.close",
		"target": workspaceSemanticTarget(number),
	})
}

func actionWorkspaceOffset(offset int) bool {
	if vtui.FrameManager == nil || len(vtui.FrameManager.Screens) < 2 {
		return false
	}
	index := vtui.FrameManager.ActiveIdx + offset
	if index < 0 {
		index = len(vtui.FrameManager.Screens) - 1
	} else if index >= len(vtui.FrameManager.Screens) {
		index = 0
	}
	return actionActivateWorkspaceNumber(vtui.FrameManager.Screens[index].Number)
}

func actionWorkspaceNext() bool {
	return actionWorkspaceOffset(1)
}

func actionWorkspacePrevious() bool {
	return actionWorkspaceOffset(-1)
}

func actionWorkspaceList() bool {
	if vtui.FrameManager == nil || len(vtui.FrameManager.Screens) == 0 {
		return false
	}
	return vtui.FrameManager.HandleSemanticAction(map[string]any{
		"action": "workspace.menu",
		"target": "workspace-counter",
	})
}

func dumpScreenTo(path string) error {
	if vtui.FrameManager == nil || vtui.FrameManager.Screen() == nil {
		return fmt.Errorf("screen buffer is not initialized")
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	vtui.FrameManager.Screen().Dump(file)
	return file.Close()
}

func actionScreenDump() bool {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return false
	}
	path := filepath.Join(home, "vtui.screen.log")
	if err := dumpScreenTo(path); err != nil {
		return false
	}
	vtui.DebugLog("FM: Screen dump saved to %s", path)
	return true
}
