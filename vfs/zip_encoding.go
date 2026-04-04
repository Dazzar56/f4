package vfs

import (
	"github.com/klauspost/compress/zip"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// DecodeZipName применяет эвристику из far2l для корректного отображения имен файлов.
func DecodeZipName(hdr *zip.FileHeader) string {
	// Если установлен бит 11 (Language encoding flag), имя уже в UTF-8.
	if (hdr.Flags & 0x800) != 0 {
		return hdr.Name
	}

	packOS := hdr.CreatorVersion >> 8
	packVer := hdr.CreatorVersion & 0xFF

	var enc encoding.Encoding

	// Логика из far2l/multiarc/src/formats/zip/zip.cpp:
	// 0 - FAT (MS-DOS), 6 - HPFS (OS/2), 11 - NTFS (Win32)
	if packOS == 11 && packVer >= 20 {
		// Win32 + версия >= 2.0 обычно пишут в ANSI (Windows-1251 для кириллицы)
		enc = charmap.Windows1251
	} else if packOS == 0 || packOS == 6 || packOS == 11 {
		// Старые системы пишут в OEM (CP866 для кириллицы)
		enc = charmap.CodePage866
	} else {
		// Стандарт ZIP требует CP437 по умолчанию
		enc = charmap.CodePage437
	}

	if enc != nil {
		out, err := enc.NewDecoder().String(hdr.Name)
		if err == nil {
			return out
		}
	}

	return hdr.Name
}