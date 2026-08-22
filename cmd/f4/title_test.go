package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/unxed/vtui"
)

func TestUpdateWindowTitle(t *testing.T) {
	// 1. Резервное копирование текущего состояния конфигурации
	origTemplate := AppConfig.ConsoleTitleTemplate
	defer func() {
		AppConfig.ConsoleTitleTemplate = origTemplate
	}()

	// 2. Инициализация кэша заголовков для детерминированности теста
	initTitleCache()

	// 3. Создание изолированного буфера экрана для перехвата вывода
	var out bytes.Buffer
	scr := vtui.NewScreenBuf()
	scr.AllocBuf(80, 25)
	scr.Writer = &out

	// See snapshotFrameManagerState for why this isn't a plain struct copy.
	defer snapshotFrameManagerState(t)()

	// Инициализируем чистый стек окон во фреймворке
	vtui.FrameManager.Init(scr)

	// Добавляем тестовый фрейм, чтобы заголовок экрана стал "Desktop"
	desktop := vtui.NewDesktop()
	vtui.FrameManager.Push(desktop)

	// A transient menu is the top frame, but it must not leak into the host
	// terminal tab title.
	menu := vtui.NewVMenu("Commands")
	vtui.FrameManager.Push(menu)

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Default Template",
			template: "f4 - %State",
			want:     "f4 - Desktop",
		},
		{
			name:     "Custom Template with User & Host",
			template: "[%User@%Host] %State",
			want:     "[" + cachedUser + "@" + cachedHost + "] Desktop",
		},
		{
			name:     "All Placeholders",
			template: "%State|%Ver|%Platform|%Backend|%Host|%User|%Admin",
			want:     "Desktop|" + cachedVersion + "|" + cachedPlat + "|" + getBackendName() + "|" + cachedHost + "|" + cachedUser + "|" + cachedAdmin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out.Reset()
			AppConfig.ConsoleTitleTemplate = tt.template

			// Имитируем проход рендеринга
			UpdateWindowTitle(scr)

			// Выталкиваем накопленные ESC-последовательности в буфер
			scr.Flush()

			got := out.String()
			// Ожидаем корректную управляющую последовательность OSC 0
			expectedSequence := "\x1b]0;" + tt.want + "\x07"
			if !strings.Contains(got, expectedSequence) {
				t.Errorf("UpdateWindowTitle() output = %q, expected to contain %q", got, expectedSequence)
			}
		})
	}
}

func TestCurrentWindowTitleMatchesRenderedTitle(t *testing.T) {
	origTemplate := AppConfig.ConsoleTitleTemplate
	defer func() { AppConfig.ConsoleTitleTemplate = origTemplate }()

	defer snapshotFrameManagerState(t)()
	scr := vtui.NewScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	vtui.FrameManager.Push(vtui.NewDesktop())

	AppConfig.ConsoleTitleTemplate = "debug %State|%Platform"
	if got, want := currentWindowTitle(), "debug Desktop|"+cachedPlat; got != want {
		t.Fatalf("currentWindowTitle() = %q, want %q", got, want)
	}
}
