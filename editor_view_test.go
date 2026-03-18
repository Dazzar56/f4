package main

import (
	"strings"
	"time"
	"os"
	"testing"
	"github.com/unxed/f4/piecetable"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
)

func TestEditorView_TypingAndBackspace(t *testing.T) {
	pt := piecetable.New([]byte("Hello"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 5 // Конец "Hello"

	// 1. Печатаем '!'
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})
	if pt.String() != "Hello!" {
		t.Errorf("Typing failed: expected 'Hello!', got '%s'", pt.String())
	}
	if ev.CursorPos != 6 {
		t.Errorf("CursorPos after typing: expected 6, got %d", ev.CursorPos)
	}

	// 2. Стираем '!' через Backspace
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "Hello" {
		t.Errorf("Backspace failed: expected 'Hello', got '%s'", pt.String())
	}
	if ev.CursorPos != 5 {
		t.Errorf("CursorPos after backspace: expected 5, got %d", ev.CursorPos)
	}
}

func TestEditorView_LineNavigation(t *testing.T) {
	pt := piecetable.New([]byte("Line1\nLine2"))
	ev := NewEditorView(pt, "")
	ev.CursorLine = 0
	ev.CursorPos = 5 // Конец "Line1"

	// 1. Стрелка Вправо в конце строки -> переход на начало следующей
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cross-line Right failed: expected Line 1, Pos 0. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Стрелка Влево в начале строки -> переход в конец предыдущей
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_LEFT})
	if ev.CursorLine != 0 || ev.CursorPos != 5 {
		t.Errorf("Cross-line Left failed: expected Line 0, Pos 5. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_EnterAndBackspaceMerging(t *testing.T) {
	pt := piecetable.New([]byte("ABC"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 1 // Между A и B

	// 1. Нажимаем Enter -> разрыв строки "A" и "BC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
	if pt.String() != "A\nBC" {
		t.Errorf("Enter splitting failed: expected 'A\\nBC', got %q", pt.String())
	}
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cursor position after Enter wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Нажимаем Backspace в начале второй строки -> склейка обратно в "ABC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "ABC" {
		t.Errorf("Backspace merging failed: expected 'ABC', got %q", pt.String())
	}
	if ev.CursorLine != 0 || ev.CursorPos != 1 {
		t.Errorf("Cursor position after merge wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_StickyColumn(t *testing.T) {
	// Создаем текст:
	// LongLine (8)
	// Short (5)
	// LongLine (8)
	pt := piecetable.New([]byte("LongLine\nShort\nLongLine"))
	ev := NewEditorView(pt, "")

	// Встаем в конец первой длинной строки
	ev.CursorLine = 0
	ev.CursorPos = 8
	ev.DesiredCursorPos = 8

	// 1. Вниз на короткую строку -> визуально в конце (5), но желаемая позиция остается 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorPos != 5 {
		t.Errorf("Down to short line: expected pos 5, got %d", ev.CursorPos)
	}
	if ev.DesiredCursorPos != 8 {
		t.Errorf("Desired position lost! Expected 8, got %d", ev.DesiredCursorPos)
	}

	// 2. Вниз на длинную строку -> позиция должна восстановиться до 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 2 || ev.CursorPos != 8 {
		t.Errorf("Sticky column failed: expected Line 2, Pos 8. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_SaveFile(t *testing.T) {
	// 1. Создаем временный файл
	tmpFile := "test_save.txt"
	defer os.Remove(tmpFile)
	err := os.WriteFile(tmpFile, []byte("Original"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Открываем его в редакторе
	pt := piecetable.New([]byte("Original"))
	ev := NewEditorView(pt, tmpFile)

	// 3. Имитируем ввод текста " + Edit" в конец
	ev.CursorPos = 8
	for _, char := range " + Edit" {
		ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: char})
	}

	// 4. Имитируем нажатие F2 (Сохранение)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F2})

	// 5. Читаем файл с диска и проверяем, что данные записались
	savedData, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Original + Edit"
	if string(savedData) != expected {
		t.Errorf("Save failed: expected %q on disk, got %q", expected, string(savedData))
	}
}
func TestEditorView_Selection(t *testing.T) {
	pt := piecetable.New([]byte("Select Me"))
	ev := NewEditorView(pt, "")
	ev.CursorLine = 0
	ev.CursorPos = 0

	// 1. Начинаем выделение (Shift + Right x 6)
	// В тесте важно эмулировать KeyDown с флагом Shift
	for i := 0; i < 6; i++ {
		ev.ProcessKey(&vtinput.InputEvent{
			Type:            vtinput.KeyEventType,
			KeyDown:         true,
			VirtualKeyCode:  vtinput.VK_RIGHT,
			ControlKeyState: vtinput.ShiftPressed,
		})
	}

	if !ev.selActive {
		t.Fatal("Selection should be active")
	}
	if ev.selAnchorOffset != 0 {
		t.Errorf("Anchor should be 0, got %d", ev.selAnchorOffset)
	}

	min, max := ev.getSelectionRange()
	if min != 0 || max != 6 {
		t.Errorf("Wrong selection range: [%d:%d]", min, max)
	}

	// 2. Копирование (Ctrl+C) - проверяем только лог или отсутствие паники
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_C,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})

	// 3. Удаление выделенного (Delete)
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_DELETE,
	})

	if pt.String() != " Me" {
		t.Errorf("Delete selection failed: %q", pt.String())
	}
	if ev.selActive {
		t.Error("Selection should be cleared after delete")
	}
}
func TestEditorView_DeleteSelectionMultiline(t *testing.T) {
	// Текст из трех строк
	pt := piecetable.New([]byte("Line1\nLine2\nLine3"))
	ev := NewEditorView(pt, "")

	// 1. Выделяем конец первой строки, всю вторую и начало третьей
	// "Line[1\nLine2\nLin]e3"
	ev.CursorLine = 0
	ev.CursorPos = 4
	ev.selActive = true
	ev.selAnchorOffset = ev.li.GetLineOffset(0) + ev.CursorPos // Офсет 4

	// Перемещаем курсор в конец выделения
	ev.CursorLine = 2
	ev.CursorPos = 3
	// Офсет начала "Line3" (12) + 3 = 15

	// 2. Удаляем выделение
	ev.DeleteSelection()

	// Ожидаемый результат: "Linee3"
	expected := "Linee3"
	if pt.String() != expected {
		t.Errorf("Multiline delete failed: expected %q, got %q", expected, pt.String())
	}

	// Проверяем, что индекс строк обновился (осталась 1 строка)
	if ev.li.LineCount() != 1 {
		t.Errorf("LineCount after multiline delete: expected 1, got %d", ev.li.LineCount())
	}

	// Проверяем позицию курсора (должен быть в точке удаления)
	if ev.CursorLine != 0 || ev.CursorPos != 4 {
		t.Errorf("Cursor after multiline delete: expected Line 0, Pos 4. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}
func TestEditorView_WordWrapNavigation(t *testing.T) {
	// Создаем одну очень длинную строку (25 символов)
	// При ширине 10 она должна разбиться на 3 визуальные строки: [0-9], [10-19], [20-24]
	text := "0123456789ABCDEFGHIJklmno"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.X1, ev.Y1, ev.X2, ev.Y2 = 0, 0, 9, 5 // Ширина 10

	ev.CursorLine = 0
	ev.CursorPos = 5 // Символ '5' в первом фрагменте
	ev.DesiredCursorPos = 5

	// 1. Нажимаем Вниз -> должны остаться на той же логической строке, но переместиться на фрагмент 2
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})

	if ev.CursorLine != 0 {
		t.Errorf("WordWrap Down: expected logical line 0, got %d", ev.CursorLine)
	}
	if ev.CursorPos != 15 { // '5' + 10 = 15 (символ 'F')
		t.Errorf("WordWrap Down: expected byte pos 15, got %d", ev.CursorPos)
	}

	// 2. Нажимаем Вверх -> возвращаемся на фрагмент 1
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
	if ev.CursorPos != 5 {
		t.Errorf("WordWrap Up: expected byte pos 5, got %d", ev.CursorPos)
	}
}
func TestEditorView_UTF8Editing(t *testing.T) {
	// "Привет" - русские буквы занимают по 2 байта
	pt := piecetable.New([]byte("Привет"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 4 // После "Пр" (4 байта)

	// 1. Вставляем еще одну букву (2 байта)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'и'})
	if ev.CursorPos != 6 {
		t.Errorf("UTF8 typing: expected pos 6, got %d", ev.CursorPos)
	}

	// 2. Backspace должен удалить ровно один символ (2 байта)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "Привет" {
		t.Errorf("UTF8 backspace failed: %q", pt.String())
	}
	if ev.CursorPos != 4 {
		t.Errorf("UTF8 backspace pos: expected 4, got %d", ev.CursorPos)
	}
}

func TestEditorView_WideCharWrap(t *testing.T) {
	// "A世B" -> A(1), 世(2), B(1). Всего 4 ячейки.
	// Ширина 2.
	// Ожидаемые фрагменты: ["A ", "世", "B "]
	// (世 не влезает в первую строку после A, должна перенестись целиком)
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true

	frags := ev.getLineFragments(0, 2)
	if len(frags) < 2 {
		t.Fatalf("Expected at least 2 fragments, got %d", len(frags))
	}

	// Проверяем, что фрагмент не заканчивается на "половине" иероглифа
	for i, f := range frags {
		for _, cell := range f.cells {
			if cell.info.Char == vtui.WideCharFiller && i == 0 {
				t.Error("WideCharFiller found at the beginning of fragment 0. Wide character was split!")
			}
		}
	}
}

func TestEditorView_SelectionWrapping(t *testing.T) {
	pt := piecetable.New([]byte("1234567890"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.X1, ev.Y1, ev.X2, ev.Y2 = 0, 0, 4, 2 // Ширина 5

	// Выделяем "456" (с 3-й по 6-ю позицию)
	// Это захватывает конец первого фрагмента "12345" и начало второго "67890"
	ev.CursorPos = 3
	ev.selActive = true
	ev.selAnchorOffset = 3
	ev.CursorPos = 6

	min, max := ev.getSelectionRange()
	if min != 3 || max != 6 {
		t.Errorf("Wrapped selection range failed: [%d:%d]", min, max)
	}
}
func TestEditorView_WideCharNavigation(t *testing.T) {
	// "A世B" -> 世 занимает 2 колонки.
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = false
	ev.CursorPos = 0 // На 'A'

	// 1. Вправо -> должны попасть на '世' (смещение 1)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorPos != 1 {
		t.Errorf("Navigate to Wide: expected pos 1, got %d", ev.CursorPos)
	}

	// 2. Вправо -> должны ПЕРЕПРЫГНУТЬ '世' (её размер 3 байта в UTF-8) и попасть на 'B' (смещение 1+3=4)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorPos != 4 {
		t.Errorf("Navigate over Wide: expected pos 4, got %d", ev.CursorPos)
	}
}
func TestEditorView_UTF8Selection(t *testing.T) {
	// "Да" - 2 руны, 4 байта
	pt := piecetable.New([]byte("Да"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 0

	// Начинаем выделение: Shift + Right (одна буква 'Д')
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_RIGHT,
		ControlKeyState: vtinput.ShiftPressed,
	})

	if !ev.selActive { t.Fatal("Selection should be active") }
	min, max := ev.getSelectionRange()
	if min != 0 || max != 2 {
		t.Errorf("UTF8 Selection failed: expected [0:2], got [%d:%d]", min, max)
	}
}
func TestEditorView_HomeEnd(t *testing.T) {
	pt := piecetable.New([]byte("Hello World"))
	ev := NewEditorView(pt, "")

	// 1. Тест End
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_END})
	if ev.CursorPos != 11 {
		t.Errorf("End failed: expected pos 11, got %d", ev.CursorPos)
	}

	// 2. Тест Home
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_HOME})
	if ev.CursorPos != 0 {
		t.Errorf("Home failed: expected pos 0, got %d", ev.CursorPos)
	}
}

func TestEditorView_WideCharBackspace(t *testing.T) {
	// "A世" -> 'A' (1), '世' (3 bytes)
	pt := piecetable.New([]byte("A世"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 4 // В самом конце

	// Нажимаем Backspace (удаляем '世')
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})

	if pt.String() != "A" {
		t.Errorf("Wide Backspace failed: expected 'A', got %q", pt.String())
	}
	if ev.CursorPos != 1 {
		t.Errorf("Wide Backspace pos failed: expected 1, got %d", ev.CursorPos)
	}
}
func TestEditorView_BracketedPaste(t *testing.T) {
	pt := piecetable.New([]byte("Start-"))
	ev := NewEditorView(pt, "")
	ev.CursorLine = 0
	ev.CursorPos = 6

	// 1. Сигнал начала вставки (PasteStart: true)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: true})
	if !ev.IsBusy() {
		t.Error("Editor should be Busy during paste")
	}

	// 2. Имитируем символы: "A", "B", Enter (\n), "C"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'A'})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'B'})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'C'})

	// ВАЖНО: Модель не должна меняться до PasteStart: false
	if pt.String() != "Start-" {
		t.Errorf("Model changed prematurely during paste: %q", pt.String())
	}

	// 3. Сигнал конца вставки (PasteStart: false)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: false})

	// Теперь всё должно быть в модели
	expected := "Start-AB\nC"
	if pt.String() != expected {
		t.Errorf("Paste commit failed: expected %q, got %q", expected, pt.String())
	}

	// Проверяем позицию курсора (строка 1, позиция 1 - после 'C')
	if ev.CursorLine != 1 || ev.CursorPos != 1 {
		t.Errorf("Post-paste cursor error: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}
func TestEditorView_ExtremeBounds(t *testing.T) {
	pt := piecetable.New([]byte("A"))
	ev := NewEditorView(pt, "")

	// 1. Backspace в начале файла не должен ничего ломать
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "A" {
		t.Error("Backspace at file start modified the text")
	}

	// 2. Delete в конце файла не должен ничего ломать
	ev.CursorPos = 1
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DELETE})
	if pt.String() != "A" {
		t.Error("Delete at file end modified the text")
	}
}

func TestEditorView_EmptyLinesWrap(t *testing.T) {
	// Файл из трех пустых строк (только переносы)
	pt := piecetable.New([]byte("\n\n"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.X1, ev.Y1, ev.X2, ev.Y2 = 0, 0, 10, 10

	if ev.li.LineCount() != 3 {
		t.Errorf("Expected 3 lines, got %d", ev.li.LineCount())
	}

	// Проверяем, что getLineFragments не возвращает nil для пустой строки
	frags := ev.getLineFragments(0, 10)
	if len(frags) == 0 {
		t.Fatal("Empty line fragments should not be empty")
	}

	// Навигация по пустым строкам
	ev.CursorLine = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 1 {
		t.Errorf("Down on empty lines failed: expected line 1, got %d", ev.CursorLine)
	}
}

func TestEditorView_WordWrapScrolling(t *testing.T) {
	// Создаем длинный текст (одна логическая строка, разбитая на 5 фрагментов)
	// Ширина 10, длина 45
	text := "0123456789ABCDEFGHIJklmnopqrstuvwxyz012345678"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	// Окно высотой 2 строки (X1, Y1, X2, Y2)
	ev.X1, ev.Y1, ev.X2, ev.Y2 = 0, 0, 9, 1 
	
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.ensureCursorVisible()
	
	if ev.ScrollTop != 0 || ev.ScrollSubLine != 0 {
		t.Error("Initial scroll should be 0:0")
	}

	// 1. Прыгаем в конец очень длинной строки (офсет 45)
	ev.CursorPos = 45
	ev.ensureCursorVisible()
	
	// Так как высота окна 2, а фрагментов 5, верхним должен стать 4-й фрагмент
	// (чтобы 4-й и 5-й фрагменты были видны на экране)
	if ev.ScrollSubLine != 3 {
		t.Errorf("WordWrap scroll failed: expected ScrollSubLine 3, got %d", ev.ScrollSubLine)
	}
	
	// 2. Прыгаем обратно в начало
	ev.CursorPos = 0
	ev.ensureCursorVisible()
	if ev.ScrollSubLine != 0 {
		t.Errorf("WordWrap scroll back failed: expected ScrollSubLine 0, got %d", ev.ScrollSubLine)
	}
}
func TestEditorView_WordWrapInfiniteLoop(t *testing.T) {
	// Текст с широким символом
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true

	// Экстремально узкое окно (ширина 1)
	// '世' занимает 2 колонки. Раньше это вызывало бесконечный цикл.
	frags := ev.getLineFragments(0, 1)

	if len(frags) == 0 {
		t.Fatal("Should have produced fragments even for narrow window")
	}
	// Проверяем, что мы не зависли и прошли всю строку
	lastFrag := frags[len(frags)-1]
	if lastFrag.endByteInLine < 5 { // A(1) + 世(3) + B(1) = 5
		t.Errorf("Fragments didn't cover the whole line: end at %d", lastFrag.endByteInLine)
	}
}
func TestEditorView_F6_ToggleWordWrap(t *testing.T) {
	pt := piecetable.New([]byte("some text"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true

	// Press F6
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F6})
	if ev.WordWrap {
		t.Error("F6 failed to disable WordWrap")
	}

	// Press F6 again
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F6})
	if !ev.WordWrap {
		t.Error("F6 failed to re-enable WordWrap")
	}
}

func TestEditorView_WideCharDelete(t *testing.T) {
	// "A世" -> 'A' (1), '世' (3 bytes)
	pt := piecetable.New([]byte("A世"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 1 // Before '世'

	// Press Delete
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DELETE})

	if pt.String() != "A" {
		t.Errorf("Wide Delete failed: expected 'A', got %q", pt.String())
	}
	if ev.CursorPos != 1 {
		t.Errorf("Cursor position after Wide Delete should remain 1, got %d", ev.CursorPos)
	}
}
func TestEditorView_LongLinePerformance(t *testing.T) {
	t.Parallel()

	// Создаем одну очень длинную строку (100 КБ), чтобы симулировать проблему.
	// Без фикса это вызывало бы O(N*M) чтений и зависание.
	longLine := strings.Repeat("a", 100*1024)
	pt := piecetable.New([]byte(longLine))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24) // 80x25 viewport

	// Устанавливаем курсор в середину строки
	ev.CursorPos = 50 * 1024

	// Оборачиваем тест в таймаут. Если редактор "зависнет", тест упадет.
	done := make(chan struct{})
	go func() {
		// Имитируем 100 нажатий "вправо". Это сильно нагрузит ensureCursorVisible.
		for i := 0; i < 100; i++ {
			ev.ProcessKey(&vtinput.InputEvent{
				Type:           vtinput.KeyEventType,
				KeyDown:        true,
				VirtualKeyCode: vtinput.VK_RIGHT,
			})
		}
		// Переход в конец строки — еще одна дорогая операция без кэширования
		ev.ProcessKey(&vtinput.InputEvent{
			Type:           vtinput.KeyEventType,
			KeyDown:        true,
			VirtualKeyCode: vtinput.VK_END,
		})
		close(done)
	}()

	select {
	case <-done:
		// Успех: все операции завершились за отведенное время.
	case <-time.After(200 * time.Millisecond): // 200мс — щедрый таймаут. Зависание длилось бы секунды.
		t.Fatal("Performance test timed out. EditorView is likely still hanging on long lines.")
	}
}
