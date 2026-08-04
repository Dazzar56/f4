package main

import (
	"strings"
	"testing"
)

func envHas(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}

func envHasKey(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

func TestChildEnvAdvertisesGraphics(t *testing.T) {
	base := []string{"PATH=/bin", "KITTY_WINDOW_ID=77", "TERM_PROGRAM=far2l"}
	env := buildChildEnv(base, true)

	if !envHas(env, "PATH=/bin") {
		t.Error("the inherited environment must survive")
	}
	if !envHas(env, "F4_NESTED=1") {
		t.Error("F4_NESTED must still be exported")
	}
	if !envHas(env, "KITTY_WINDOW_ID=1") || envHas(env, "KITTY_WINDOW_ID=77") {
		t.Errorf("the graphics advertisement must be ours, got %v", env)
	}
	if !envHas(env, "TERM_PROGRAM=f4") || envHas(env, "TERM_PROGRAM=far2l") {
		t.Errorf("the program talks to f4, not to what started it: %v", env)
	}
}

func TestChildEnvKeepsQuietWithoutGraphics(t *testing.T) {
	base := []string{"PATH=/bin", "KITTY_WINDOW_ID=77"}
	env := buildChildEnv(base, false)

	if envHasKey(env, "KITTY_WINDOW_ID") {
		t.Errorf("a terminal that cannot show pictures must not claim it can: %v", env)
	}
	if !envHas(env, "F4_NESTED=1") || !envHas(env, "PATH=/bin") {
		t.Errorf("the rest of the environment must be untouched: %v", env)
	}
}