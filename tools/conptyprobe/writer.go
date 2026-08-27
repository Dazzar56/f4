package main

import (
	"fmt"
	"os"
	"strings"
)

// writer buffers the whole log so the summary can be printed *first* in the
// file: whoever reads the issue should not have to scroll 60 KB to find the
// six lines that matter.
type writer struct {
	body strings.Builder
	sum  []kv
}

type kv struct{ k, v string }

func (w *writer) printf(format string, a ...any) {
	fmt.Fprintf(&w.body, format, a...)
}

func (w *writer) section(title string) {
	w.printf("\n\n========================================================\n")
	w.printf("== %s\n", title)
	w.printf("========================================================\n")
}

func (w *writer) sub(title string) {
	w.printf("\n---- %s ----\n", title)
}

// summary records one answer. Keys are stable so logs from different machines
// can be diffed against each other.
func (w *writer) summary(k, v string) {
	for i := range w.sum {
		if w.sum[i].k == k {
			w.sum[i].v = v
			return
		}
	}
	w.sum = append(w.sum, kv{k, v})
}

// step prints progress on screen only: the person running this needs to see
// that it is alive, not the bytes.
func (w *writer) step(format string, a ...any) {
	fmt.Printf(format+"\n", a...)
}

func (w *writer) summaryBlock() string {
	var sb strings.Builder
	width := 0
	for _, e := range w.sum {
		if len(e.k) > width {
			width = len(e.k)
		}
	}
	sb.WriteString("======== SUMMARY (paste this if the full log is too big) ========\n")
	for _, e := range w.sum {
		fmt.Fprintf(&sb, "%-*s = %s\n", width, e.k, e.v)
	}
	sb.WriteString("================================================================\n")
	return sb.String()
}

func (w *writer) save(path string) error {
	return os.WriteFile(path, []byte(w.summaryBlock()+w.body.String()), 0644)
}
