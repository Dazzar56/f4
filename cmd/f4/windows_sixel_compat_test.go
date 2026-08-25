package main

import "testing"

func TestConfigureWindowsTerminalSixel(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
		set  bool
	}{
		{name: "not Windows Terminal", env: map[string]string{}, want: ""},
		{name: "default", env: map[string]string{"WT_SESSION": "session"}, want: "adaptive", set: true},
		{name: "explicit fixed", env: map[string]string{"WT_SESSION": "session", "VTUI_SIXEL_PALETTE": "fixed"}, want: "fixed"},
		{name: "explicit true color", env: map[string]string{"WT_SESSION": "session", "VTUI_SIXEL_PALETTE": "truecolor"}, want: "truecolor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := func(key string) string { return tc.env[key] }
			set := false
			setenv := func(key, value string) error {
				set = true
				tc.env[key] = value
				return nil
			}
			configureWindowsTerminalSixelWith(env, setenv)
			if got := tc.env["VTUI_SIXEL_PALETTE"]; got != tc.want {
				t.Fatalf("palette mode = %q, want %q", got, tc.want)
			}
			if set != tc.set {
				t.Fatalf("setenv called = %v, want %v", set, tc.set)
			}
		})
	}
}
