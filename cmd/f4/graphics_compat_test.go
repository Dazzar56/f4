package main

import (
	"testing"

	"github.com/unxed/vtui"
)

func graphicsCompatEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func newGraphicsCompatScreen() *vtui.ScreenBuf {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	return scr
}

func TestPreferCompatibleGraphicsProtocolUsesKittyInKonsole(t *testing.T) {
	scr := newGraphicsCompatScreen()
	scr.Graphics().SetProtocol(vtui.GraphicsSixel)
	env := graphicsCompatEnv(map[string]string{"KONSOLE_VERSION": "230805"})

	preferCompatibleGraphicsProtocol(scr, env)

	if got := scr.Graphics().Protocol(); got != vtui.GraphicsKitty {
		t.Fatalf("Konsole protocol: got %v, want kitty", got)
	}
}

func TestPreferCompatibleGraphicsProtocolKeepsExplicitProtocol(t *testing.T) {
	for _, forced := range []string{"sixel", "kitty", "none"} {
		t.Run(forced, func(t *testing.T) {
			scr := newGraphicsCompatScreen()
			want, ok := vtui.ParseGraphicsProtocol(forced)
			if !ok {
				t.Fatalf("test protocol %q is invalid", forced)
			}
			scr.Graphics().SetProtocol(want)
			env := graphicsCompatEnv(map[string]string{
				"KONSOLE_VERSION": "230805",
				"VTUI_GRAPHICS":   forced,
			})

			preferCompatibleGraphicsProtocol(scr, env)

			if got := scr.Graphics().Protocol(); got != want {
				t.Fatalf("explicit protocol: got %v, want %v", got, want)
			}
		})
	}
}

func TestPreferCompatibleGraphicsProtocolLeavesOtherTerminalsAlone(t *testing.T) {
	scr := newGraphicsCompatScreen()
	scr.Graphics().SetProtocol(vtui.GraphicsNone)
	env := graphicsCompatEnv(map[string]string{"TERM": "xterm-256color"})

	preferCompatibleGraphicsProtocol(scr, env)

	if got := scr.Graphics().Protocol(); got != vtui.GraphicsNone {
		t.Fatalf("non-Konsole protocol: got %v, want none", got)
	}
}

func TestPreferCompatibleGraphicsProtocolLeavesOldKonsoleOnSixel(t *testing.T) {
	scr := newGraphicsCompatScreen()
	scr.Graphics().SetProtocol(vtui.GraphicsSixel)
	env := graphicsCompatEnv(map[string]string{"KONSOLE_VERSION": "220300"})

	preferCompatibleGraphicsProtocol(scr, env)

	if got := scr.Graphics().Protocol(); got != vtui.GraphicsSixel {
		t.Fatalf("old Konsole protocol: got %v, want sixel", got)
	}
}
