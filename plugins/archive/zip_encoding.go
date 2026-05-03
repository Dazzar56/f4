package archive

import (
	"github.com/klauspost/compress/zip"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

func DecodeZipName(hdr *zip.FileHeader) string {
	if (hdr.Flags & 0x800) != 0 {
		return hdr.Name
	}
	packOS := hdr.CreatorVersion >> 8
	packVer := hdr.CreatorVersion & 0xFF
	var enc encoding.Encoding
	if packOS == 11 && packVer >= 20 {
		enc = charmap.Windows1251
	} else if packOS == 0 || packOS == 6 || packOS == 11 {
		enc = charmap.CodePage866
	} else {
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
