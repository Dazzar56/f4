package piecetable

import "sort"

// LineIndex хранит смещения начала каждой строки.
type LineIndex struct {
	offsets []int
}

// NewLineIndex создает новый пустой индекс.
func NewLineIndex() *LineIndex {
	return &LineIndex{
		offsets: []int{0},
	}
}

// Rebuild полностью перестраивает индекс строк на основе PieceTable.
func (li *LineIndex) Rebuild(pt *PieceTable) {
	// Сбрасываем индекс, первая строка всегда начинается с 0
	li.offsets = []int{0}

	if pt.Size() == 0 {
		return
	}

	absPos := 0
	pt.ForEachRange(func(data []byte) {
		for i, b := range data {
			if b == '\n' {
				// Следующая строка начинается сразу за символом переноса
				li.offsets = append(li.offsets, absPos+i+1)
			}
		}
		absPos += len(data)
	})
}

// LineCount возвращает общее количество строк.
func (li *LineIndex) LineCount() int {
	return len(li.offsets)
}

// GetLineOffset возвращает байтовое смещение начала указанной строки (0-based).
func (li *LineIndex) GetLineOffset(line int) int {
	if line < 0 || line >= len(li.offsets) {
		return -1
	}
	return li.offsets[line]
}

// GetLineAtOffset возвращает номер строки (0-based), которой принадлежит указанное смещение.
// Использует бинарный поиск для скорости O(log N).
func (li *LineIndex) GetLineAtOffset(offset int) int {
	if offset <= 0 {
		return 0
	}

	// Поиск первого индекса i, для которого li.offsets[i] > offset
	idx := sort.Search(len(li.offsets), func(i int) bool {
		return li.offsets[i] > offset
	})

	// Номер строки — это idx - 1
	return idx - 1
}

// UpdateAfterInsert инкрементально обновляет индекс после вставки данных.
func (li *LineIndex) UpdateAfterInsert(offset int, data []byte) {
	lenData := len(data)
	if lenData == 0 {
		return
	}

	// 1. Находим строку, в которую была вставка
	lineIdx := li.GetLineAtOffset(offset)

	// 2. Ищем новые переносы строк во вставленном фрагменте
	var newOffsets []int
	currentOffset := offset
	for _, b := range data {
		currentOffset++
		if b == '\n' {
			newOffsets = append(newOffsets, currentOffset)
		}
	}

	// 3. Сдвигаем все последующие смещения
	for i := lineIdx + 1; i < len(li.offsets); i++ {
		li.offsets[i] += lenData
	}

	// 4. Вставляем новые смещения строк, если они были
	if len(newOffsets) > 0 {
		// Создаем новый слайс для вставки
		tail := append(newOffsets, li.offsets[lineIdx+1:]...)
		li.offsets = append(li.offsets[:lineIdx+1], tail...)
	}
}

// UpdateAfterDelete инкрементально обновляет индекс после удаления данных.
func (li *LineIndex) UpdateAfterDelete(offset, length int) {
	if length == 0 {
		return
	}

	startLine := li.GetLineAtOffset(offset)
	endLine := li.GetLineAtOffset(offset + length)

	// 1. Определяем, сколько строк было удалено
	linesRemoved := endLine - startLine

	// 2. Сдвигаем все последующие смещения
	for i := endLine + 1; i < len(li.offsets); i++ {
		li.offsets[i] -= length
	}

	// 3. Удаляем смещения "схлопнувшихся" строк
	if linesRemoved > 0 {
		li.offsets = append(li.offsets[:startLine+1], li.offsets[endLine+1:]...)
	}
}
