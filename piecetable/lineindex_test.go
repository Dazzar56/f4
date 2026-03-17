package piecetable

import "testing"

func TestLineIndex_Build(t *testing.T) {
	// Текст:
	// Line 1 (6 bytes: L,i,n,e,1,\n)
	// Line 2 (6 bytes: L,i,n,e,2,\n)
	// Line 3 (5 bytes: L,i,n,e,3)
	pt := New([]byte("Line 1\nLine 2\nLine 3"))
	li := NewLineIndex()
	li.Rebuild(pt)

	if li.LineCount() != 3 {
		t.Errorf("Expected 3 lines, got %d", li.LineCount())
	}

	// Проверка смещений
	if li.GetLineOffset(0) != 0 {
		t.Errorf("Line 0 offset: expected 0, got %d", li.GetLineOffset(0))
	}
	if li.GetLineOffset(1) != 7 { // "Line 1\n" -> 7 bytes
		t.Errorf("Line 1 offset: expected 7, got %d", li.GetLineOffset(1))
	}
	if li.GetLineOffset(2) != 14 { // "Line 1\nLine 2\n" -> 14 bytes
		t.Errorf("Line 2 offset: expected 14, got %d", li.GetLineOffset(2))
	}
}

func TestLineIndex_GetLineAtOffset(t *testing.T) {
	pt := New([]byte("AAA\nBBB\nCCC"))
	li := NewLineIndex()
	li.Rebuild(pt)
	// Offsets: [0, 4, 8]

	tests := []struct {
		offset int
		want   int
	}{
		{0, 0}, {1, 0}, {3, 0},
		{4, 1}, {5, 1}, {7, 1},
		{8, 2}, {10, 2},
	}

	for _, tt := range tests {
		got := li.GetLineAtOffset(tt.offset)
		if got != tt.want {
			t.Errorf("At offset %d: expected line %d, got %d", tt.offset, tt.want, got)
		}
	}
}
func TestLineIndex_AppendAtEOF(t *testing.T) {
	// Проверка вставки в конец файла без \n
	pt := New([]byte("NoNewline"))
	li := NewLineIndex()
	li.Rebuild(pt)

	insertData := []byte(" + More")
	pt.Insert(9, insertData)
	li.UpdateAfterInsert(9, insertData)

	if li.LineCount() != 1 {
		t.Errorf("Expected 1 line, got %d", li.LineCount())
	}

	// Вставляем \n в середину
	newline := []byte("\n")
	pt.Insert(2, newline)
	li.UpdateAfterInsert(2, newline)

	if li.LineCount() != 2 {
		t.Errorf("Expected 2 lines after inserting newline, got %d", li.LineCount())
	}
}

func TestLineIndex_Empty(t *testing.T) {
	pt := New([]byte(""))
	li := NewLineIndex()
	li.Rebuild(pt)

	if li.LineCount() != 1 {
		t.Errorf("Empty file should have 1 line, got %d", li.LineCount())
	}
	if li.GetLineOffset(0) != 0 {
		t.Error("Line 0 offset should be 0 even for empty file")
	}
}

func TestLineIndex_DeepConsistency(t *testing.T) {
	// Проверяем, что серия инкрементальных обновлений дает тот же результат,
	// что и полный Rebuild.
	text := []byte("Line 1\nLine 2\nLine 3")
	pt := New(text)
	li := NewLineIndex()
	li.Rebuild(pt)
	
	// 1. Вставка в середину с переносом
	insertData := []byte("New\nData")
	offset := 7 // Начало "Line 2"
	pt.Insert(offset, insertData)
	li.UpdateAfterInsert(offset, insertData)
	
	// 2. Удаление части текста
	pt.Delete(2, 10)
	li.UpdateAfterDelete(2, 10)
	
	// Сравниваем с эталоном
	liExpected := NewLineIndex()
	liExpected.Rebuild(pt)
	
	if li.LineCount() != liExpected.LineCount() {
		t.Errorf("Consistency fail: LineCount %d != %d", li.LineCount(), liExpected.LineCount())
	}
	
	for i := 0; i < li.LineCount(); i++ {
		if li.GetLineOffset(i) != liExpected.GetLineOffset(i) {
			t.Errorf("Consistency fail at line %d: offset %d != %d", i, li.GetLineOffset(i), liExpected.GetLineOffset(i))
		}
	}
}
func TestLineIndex_IncrementalStress(t *testing.T) {
	// Псевдо-рандомный тест на выносливость индекса
	pt := New([]byte("Initial Text\nLine 2\nLine 3"))
	li := NewLineIndex()
	li.Rebuild(pt)

	ops := []struct {
		insert bool
		off    int
		data   string
	}{
		{true, 5, "!!!\n!!!"},
		{false, 2, "12345"}, // удаление 5 байт со смещения 2
		{true, 0, "\nStart\n"},
		{false, 10, "1"},
		{true, 15, "End"},
	}

	for i, op := range ops {
		if op.insert {
			data := []byte(op.data)
			pt.Insert(op.off, data)
			li.UpdateAfterInsert(op.off, data)
		} else {
			length := len(op.data)
			pt.Delete(op.off, length)
			li.UpdateAfterDelete(op.off, length)
		}

		// Сравнение с честным Rebuild на каждом шаге
		liRef := NewLineIndex()
		liRef.Rebuild(pt)

		if li.LineCount() != liRef.LineCount() {
			t.Fatalf("Stress step %d: LineCount mismatch. Got %d, want %d", i, li.LineCount(), liRef.LineCount())
		}
		for j := 0; j < li.LineCount(); j++ {
			if li.GetLineOffset(j) != liRef.GetLineOffset(j) {
				t.Fatalf("Stress step %d: Offset mismatch at line %d", i, j)
			}
		}
	}
}
