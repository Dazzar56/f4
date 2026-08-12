package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/f4/vtvibe"
	"github.com/unxed/vtui"
)

// vtvibe is the AI panel of f4. This file is the only place where it touches
// the core: it publishes the ai:// drive, the "ai:" command prefix and the
// registry actions. Everything else lives in the vtvibe package.

const (
	vtvibeIniName        = "vtvibe.ini"
	vtvibeDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	vtvibeDefaultModel   = "gemini-3.6-flash"
)

var (
	vtvibeOnce    sync.Once
	vtvibeSession *vtvibe.Session
)

// aiSession returns the single dialog shared by every ai:// mount.
func aiSession() *vtvibe.Session {
	vtvibeOnce.Do(func() { vtvibeSession = vtvibe.NewSession() })
	return vtvibeSession
}

func vtvibeIniPath() string {
	return filepath.Join(GetF4ConfigDir(), vtvibeIniName)
}

// vtvibeConfig re-reads the settings on every use, so editing vtvibe.ini or
// exporting a key does not need a restart.
func vtvibeConfig() (vtvibe.Config, string) {
	ini := LoadIni(vtvibeIniPath())
	cfg := vtvibe.Config{
		BaseURL: ini.GetString("general", "base_url", vtvibeDefaultBaseURL),
		Model:   ini.GetString("general", "model", vtvibeDefaultModel),
	}
	keySource := ""
	for _, name := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			cfg.APIKey, keySource = v, name
			break
		}
	}
	if cfg.APIKey == "" {
		if v := strings.TrimSpace(ini.GetString("general", "key", "")); v != "" {
			cfg.APIKey, keySource = v, vtvibeIniName
		}
	}
	aiSession().SetStatus(vtvibe.Status{BaseURL: cfg.BaseURL, Model: cfg.Model, KeySource: keySource})
	return cfg, keySource
}

// vtvibeSaveSetting rewrites one key of vtvibe.ini, keeping the rest.
func vtvibeSaveSetting(key, value string) error {
	path := vtvibeIniPath()
	ini := LoadIni(path)
	if ini.data["general"] == nil {
		ini.data["general"] = map[string]string{}
	}
	ini.data["general"][key] = value

	keys := make([]string, 0, len(ini.data["general"]))
	for k := range ini.data["general"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("[general]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s = %s\n", k, ini.data["general"][k])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// 0600: the file may hold an API key.
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

func init() {
	RegisterDrive("AI", func() vfs.VFS { return vtvibe.NewVFS(aiSession()) })

	if _, err := (&coreAPI{}).RegisterCommandPrefix("vtvibe", "ai", aiCommand); err != nil {
		vtui.DebugLog("VTVIBE: cannot register the ai: prefix: %v", err)
	}

	withAI := func(fn func(pf *PanelsFrame)) func() bool {
		return func() bool {
			if pf := findPanelsFrameAnyScreen(); pf != nil {
				fn(pf)
				return true
			}
			return false
		}
	}

	RegisterAction(Action{
		Name:        "AI.TogglePanel",
		Area:        "Shell",
		Label:       "AI Panel",
		LabelKey:    "Action.AI.TogglePanel",
		Description: "Open or close the AI panel on the passive panel",
		DescKey:     "Action.AI.TogglePanel.Desc",
		DefaultKeys: []string{"CtrlAltA"},
		MenuPath:    "Commands",
		Handler:     withAI(func(pf *PanelsFrame) { aiTogglePanel(pf) }),
	})
	RegisterAction(Action{
		Name:        "AI.Ask",
		Area:        "Shell",
		Label:       "Ask the AI",
		LabelKey:    "Action.AI.Ask",
		Description: "Type a question and send it together with the ai:// context",
		DescKey:     "Action.AI.Ask.Desc",
		MenuPath:    "Commands",
		Handler:     withAI(func(pf *PanelsFrame) { aiAskDialog(pf) }),
	})
	RegisterAction(Action{
		Name:        "AI.NewSession",
		Area:        "Shell",
		Label:       "New AI Dialog",
		LabelKey:    "Action.AI.NewSession",
		Description: "Clear the AI dialog history and artifacts, keeping the context files",
		DescKey:     "Action.AI.NewSession.Desc",
		MenuPath:    "Commands",
		Handler:     withAI(func(pf *PanelsFrame) { aiNewSession(pf) }),
	})
	RegisterAction(Action{
		Name:        "AI.Setup",
		Area:        "Shell",
		Label:       "AI Setup",
		LabelKey:    "Action.AI.Setup",
		Description: "Set the API key and the model used by the AI panel",
		DescKey:     "Action.AI.Setup.Desc",
		MenuPath:    "Commands",
		Handler:     withAI(func(pf *PanelsFrame) { aiSetupDialog(pf) }),
	})
}

// aiPrevPath remembers where a panel was before it showed the dialog, so the
// same key brings the files back.
var aiPrevPath [2]string

func aiTogglePanel(pf *PanelsFrame) {
	fsp := pf.getInactivePanel()
	if fsp == nil {
		return
	}
	idx := 1 - pf.activeIdx
	if _, isAI := fsp.vfs.(*vtvibe.AIVFS); isAI {
		target := aiPrevPath[idx]
		if target == "" {
			target, _ = os.UserHomeDir()
		}
		pf.switchToVFS(fsp, vfs.NewOSVFS(target))
		return
	}
	aiPrevPath[idx] = fsp.vfs.GetPath()
	vtvibeConfig()
	pf.switchToVFS(fsp, vtvibe.NewVFS(aiSession()))
}

func aiNewSession(pf *PanelsFrame) {
	aiSession().Reset(true)
	vtvibeConfig()
	pf.RefreshAll()
	vtui.ShowMessage(Msg("AI.Title"), Msg("AI.NewSessionDone"), []string{Msg("vtui.Ok")})
}

func aiAskDialog(pf *PanelsFrame) {
	vtui.InputBox(Msg("AI.Title"), Msg("AI.AskPrompt"), "", func(text string) {
		if strings.TrimSpace(text) != "" {
			aiSend(pf, text)
		}
	})
}

// aiSetupDialog is the whole first-run wizard at MVP scale: paste a key, name
// a model, done. Both steps may be skipped with an empty answer.
func aiSetupDialog(pf *PanelsFrame) {
	cfg, _ := vtvibeConfig()
	vtui.InputBox(Msg("AI.Title"), Msg("AI.KeyPrompt"), "", func(key string) {
		if key = strings.TrimSpace(key); key != "" {
			if err := vtvibeSaveSetting("key", key); err != nil {
				aiShowError(err)
				return
			}
		}
		vtui.InputBox(Msg("AI.Title"), Msg("AI.ModelPrompt"), cfg.Model, func(model string) {
			if model = strings.TrimSpace(model); model != "" {
				if err := vtvibeSaveSetting("model", model); err != nil {
					aiShowError(err)
					return
				}
			}
			vtvibeConfig()
			pf.RefreshAll()
			vtui.ShowMessage(Msg("AI.Title"), fmt.Sprintf(Msg("AI.Saved"), vtvibeIniPath()), []string{Msg("vtui.Ok")})
		})
	})
}

// aiCommand handles everything typed after "ai:" in the command line. Plain
// text is a question; the few reserved words are the settings the MVP needs.
func aiCommand(app vfs.App, arg string) {
	pf := findPanelsFrameAnyScreen()
	if pf == nil {
		return
	}
	arg = strings.TrimSpace(arg)
	lower := strings.ToLower(arg)

	switch {
	case arg == "":
		draft := aiSession().Draft()
		if draft == "" {
			vtui.ShowMessage(Msg("AI.Title"), Msg("AI.EmptyDraft"), []string{Msg("vtui.Ok")})
			return
		}
		aiSend(pf, draft)
		aiSession().ClearDraft()
	case lower == "help" || lower == "?":
		vtui.ShowMessage(Msg("AI.Title"), Msg("AI.Help"), []string{Msg("vtui.Ok")})
	case lower == "new":
		aiNewSession(pf)
	case lower == "key":
		aiSetupDialog(pf)
	case lower == "models":
		aiListModels(pf)
	case lower == "model":
		aiSetupDialog(pf)
	case strings.HasPrefix(lower, "model "):
		name := strings.TrimSpace(arg[len("model "):])
		if err := vtvibeSaveSetting("model", name); err != nil {
			aiShowError(err)
			return
		}
		vtvibeConfig()
		pf.RefreshAll()
		vtui.ShowMessage(Msg("AI.Title"), fmt.Sprintf(Msg("AI.Saved"), vtvibeIniPath()), []string{Msg("vtui.Ok")})
	default:
		aiSend(pf, arg)
	}
}

// aiSend runs the round trip through the background task manager: the UI
// thread never blocks and Cancel actually cancels the HTTP request.
func aiSend(pf *PanelsFrame, question string) {
	cfg, keySource := vtvibeConfig()
	if cfg.APIKey == "" && keySource == "" && !strings.Contains(cfg.BaseURL, "127.0.0.1") &&
		!strings.Contains(cfg.BaseURL, "localhost") {
		vtui.ShowMessage(Msg("AI.Title"), Msg("AI.NoKey"), []string{Msg("vtui.Ok")})
		return
	}
	if aiSession().Busy() {
		vtui.ShowMessage(Msg("AI.Title"), Msg("AI.Busy"), []string{Msg("vtui.Ok")})
		return
	}

	session := aiSession()
	pf.RunProgressTask(Msg("AI.Title"), Msg("AI.Sending"), false,
		func(ctx context.Context, update func(msg string, percent int)) error {
			return session.Ask(ctx, cfg, question)
		},
		func(err error) {
			if err != nil {
				if err == context.Canceled {
					return
				}
				aiShowError(err)
				return
			}
			pf.RefreshAll()
			if path := aiLastAnswerPath(session); path != "" {
				actionOpenViewer(pf, vtvibe.NewVFS(session), path)
			}
		})
}

// aiLastAnswerPath is the chat file the reply was just written to.
func aiLastAnswerPath(s *vtvibe.Session) string {
	n := len(s.Turns())
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("/chat/%04d-model.md", n)
}

func aiListModels(pf *PanelsFrame) {
	cfg, _ := vtvibeConfig()
	var models []string
	pf.RunProgressTask(Msg("AI.Title"), Msg("AI.Sending"), false,
		func(ctx context.Context, update func(msg string, percent int)) error {
			list, err := cfg.Models(ctx)
			models = list
			return err
		},
		func(err error) {
			if err != nil {
				if err != context.Canceled {
					aiShowError(err)
				}
				return
			}
			if len(models) == 0 {
				vtui.ShowMessage(Msg("AI.Title"), Msg("AI.NoModels"), []string{Msg("vtui.Ok")})
				return
			}
			if len(models) > 40 {
				models = models[:40]
			}
			vtui.ShowMessage(Msg("AI.Title"), strings.Join(models, "\n"), []string{Msg("vtui.Ok")})
		})
}

func aiShowError(err error) {
	msg := err.Error()
	if err == vtvibe.ErrNoKey {
		msg = Msg("AI.NoKey")
	}
	if len(msg) > 600 {
		msg = msg[:600] + "..."
	}
	vtui.ShowMessage(Msg("AI.ErrorTitle"), msg, []string{Msg("vtui.Ok")})
}
