package vfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	stdunicode "unicode"
	"unicode/utf8"

	"github.com/abadojack/whatlanggo"
	"github.com/unxed/vtui"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

type Codepage struct {
	ID   int
	Name string
	Enc  encoding.Encoding
}

var AvailableCodepages []Codepage

const UTF8BOMSize = 3

func init() {
	AvailableCodepages = []Codepage{
		{65001, "UTF-8", unicode.UTF8},
		{11111, "1251 ANSI (Cyrillic)", nil},
		{22222, "866 OEM (Russian)", nil},
		{1200, "UTF-16 (Little endian)", unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)},
		{1201, "UTF-16 (Big endian)", unicode.UTF16(unicode.BigEndian, unicode.UseBOM)},
		{1251, "Windows-1251 (Cyrillic)", charmap.Windows1251},
		{866, "CP866 (Cyrillic OEM)", charmap.CodePage866},
		{20866, "KOI8-R (Cyrillic)", charmap.KOI8R},
		{1252, "Windows-1252 (Western)", charmap.Windows1252},
		{437, "CP437 (US OEM)", charmap.CodePage437},
		{850, "CP850 (Western OEM)", charmap.CodePage850},
		{852, "CP852 (Slavic OEM)", charmap.CodePage852},
	}
}

func DisplayCodepageName(id int) string {
	if id == 11111 {
		return "ANSI"
	}
	if id == 22222 {
		return "OEM"
	}
	if id == 65001 {
		return "UTF-8"
	}
	if cp, ok := FindCodepage(id); ok {
		return cp.Name
	}
	return fmt.Sprintf("%d", id)
}

func FindCodepage(id int) (Codepage, bool) {
	for _, cp := range AvailableCodepages {
		if cp.ID == id {
			return cp, true
		}
	}
	return Codepage{}, false
}

func DecodeBytes(data []byte, cpID int) ([]byte, error) {
	if cpID == 65001 {
		return data, nil
	}

	var decoder *encoding.Decoder
	switch cpID {
	case 11111:
		decoder = GetSystemANSIEncoding().NewDecoder()
	case 22222:
		decoder = GetSystemOEMEncoding().NewDecoder()
	default:
		cp, ok := FindCodepage(cpID)
		if !ok || cp.Enc == nil {
			return data, fmt.Errorf("unsupported codepage: %d", cpID)
		}
		decoder = cp.Enc.NewDecoder()
	}

	if decoder == nil {
		return data, fmt.Errorf("decoder is nil for codepage: %d", cpID)
	}

	return decoder.Bytes(data)
}

func EncodeBytes(data []byte, cpID int) ([]byte, error) {
	if cpID == 65001 {
		return data, nil
	}

	var encoder *encoding.Encoder
	switch cpID {
	case 11111:
		encoder = GetSystemANSIEncoding().NewEncoder()
	case 22222:
		encoder = GetSystemOEMEncoding().NewEncoder()
	default:
		cp, ok := FindCodepage(cpID)
		if !ok || cp.Enc == nil {
			return data, fmt.Errorf("unsupported codepage: %d", cpID)
		}
		encoder = cp.Enc.NewEncoder()
	}

	if encoder == nil {
		return data, fmt.Errorf("encoder is nil for codepage: %d", cpID)
	}

	return encoder.Bytes(data)
}

func DetectBOM(data []byte) (int, bool) {
	if HasUTF8BOM(data) {
		return 65001, true
	}
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			return 1200, true
		}
		if data[0] == 0xFE && data[1] == 0xFF {
			return 1201, true
		}
	}
	return 65001, false
}

// HasUTF8BOM reports whether data starts with the UTF-8 byte-order mark.
// Detection and removal are separate because the marker is useful for
// choosing the codepage, while it is not part of the text shown to a user.
func HasUTF8BOM(data []byte) bool {
	return len(data) >= UTF8BOMSize &&
		data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF
}

// StripUTF8BOM returns data without a leading UTF-8 byte-order mark. It keeps
// the original slice when there is no marker, so callers can use it on hot
// paths without an allocation.
func StripUTF8BOM(data []byte) []byte {
	if !HasUTF8BOM(data) {
		return data
	}
	return data[UTF8BOMSize:]
}

func DetectEncoding(data []byte, autodetect bool, defaultCP int) int {
	if cp, ok := DetectBOM(data); ok {
		return cp
	}
	if autodetect {
		if utf8.Valid(data) {
			return 65001
		}
		if cp, ok := detectLegacyCodepage(data); ok {
			return cp
		}
		return defaultCP
	}
	return defaultCP
}

// detectLegacyCodepage looks at every single-byte codepage f4 can decode. A
// byte stream does not carry its codepage, so this remains deliberately
// conservative: text quality is the primary signal and language detection is
// used only to break a close tie. An uncertain result falls back to the
// user's configured default rather than turning arbitrary binary data into
// text.
func detectLegacyCodepage(data []byte) (int, bool) {
	if len(data) < 8 {
		return 0, false
	}

	// Keep the system aliases first. When an alias and its explicit codepage
	// decode to the same text, the alias is the useful result on that system
	// (and preserves the existing ANSI/OEM behaviour). Explicit codepages are
	// still tried when the system locale is unrelated to the file.
	ids := []int{11111, 22222, 1251, 866, 20866, 1252, 437, 850, 852}
	type candidate struct {
		id         int
		text       string
		score      int
		confidence float64
	}
	candidates := make([]candidate, 0, len(ids))
	seenText := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		decoded, err := DecodeBytes(data, id)
		if err != nil {
			continue
		}
		text := string(decoded)
		if _, seen := seenText[text]; seen {
			continue
		}
		seenText[text] = struct{}{}
		candidates = append(candidates, candidate{
			id:    id,
			text:  text,
			score: legacyTextScore(decoded),
		})
	}
	if len(candidates) == 0 {
		return 0, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	top := candidates[0]
	if top.score <= 0 {
		return 0, false
	}
	if len(candidates) == 1 || top.score-candidates[1].score >= 8 {
		return top.id, true
	}

	// The common Cyrillic case is a genuine tie: CP1251 and KOI8-R can both
	// produce equally readable Unicode text. whatlanggo is intentionally not
	// allowed to overrule a clear text-quality win; it only ranks candidates
	// whose score is within the same conservative ambiguity window.
	for i := range candidates {
		if top.score-candidates[i].score >= 8 {
			break
		}
		candidates[i].confidence = legacyLanguageConfidence(candidates[i].text)
	}
	// candidates[0] was copied into top before the tie-breaker scores were
	// calculated; refresh the copy before comparing it with the other entries.
	top = candidates[0]
	best := top
	for _, c := range candidates[1:] {
		if top.score-c.score >= 8 {
			break
		}
		if c.confidence > best.confidence {
			best = c
		}
	}
	if best.id != top.id && best.confidence-top.confidence >= 0.05 {
		return best.id, true
	}
	if best.id == top.id && best.confidence >= 0.05 {
		return best.id, true
	}
	// When the system alias itself is one of the equally readable candidates,
	// prefer it over an explicit duplicate. This keeps ANSI/OEM detection
	// stable for the user's locale while still allowing an explicit codepage
	// to win whenever it is materially more plausible.
	if top.id == 11111 || top.id == 22222 {
		return top.id, true
	}
	return 0, false
}

// legacyLanguageConfidence averages detection over words instead of asking
// whatlanggo to classify one long fragment. A wrong codepage can accidentally
// form a convincing sequence across an entire fragment; word-level scores are
// less sensitive to that effect and still provide a useful tie-breaker for
// Cyrillic encodings. Punctuation and numeric-only fragments are ignored.
func legacyLanguageConfidence(text string) float64 {
	text = legacyLanguageSample(text)
	latin, cyrillic := 0, 0
	for _, r := range text {
		switch {
		case r >= 0x0041 && r <= 0x024F && stdunicode.IsLetter(r):
			latin++
		case r >= 0x0400 && r <= 0x052F && stdunicode.IsLetter(r):
			cyrillic++
		}
	}
	// For Latin text whatlanggo is more reliable on the complete fragment;
	// short word-level samples tend to lose the accents that distinguish
	// Windows-1252/CP850/CP852. Cyrillic candidates are scored word by word,
	// because a full wrong-codepage fragment can look like another real
	// Cyrillic language.
	if latin > cyrillic {
		return whatlanggo.Detect(text).Confidence
	}

	var total float64
	words := 0
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return stdunicode.IsSpace(r) || stdunicode.IsPunct(r)
	}) {
		if words >= 32 {
			break
		}
		if utf8.RuneCountInString(word) < 3 {
			continue
		}
		hasLetter := false
		for _, r := range word {
			if stdunicode.IsLetter(r) {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			continue
		}
		total += whatlanggo.Detect(word).Confidence
		words++
	}
	if words == 0 {
		return 0
	}
	confidence := total / float64(words)
	// The supported Cyrillic codepages are primarily used for Russian text,
	// and CP1251/KOI8-R can otherwise decode the same bytes into equally
	// readable-looking Slavic text. Prefer a candidate with common Russian
	// n-grams, but keep the bonus small enough that it cannot overturn a clear
	// text-quality result.
	if whatlanggo.Detect(text).Lang == whatlanggo.Rus {
		confidence += 0.15
	}
	confidence += legacyRussianNgramConfidence(text)
	return confidence
}

// This compact set of frequent Russian trigrams is used only as a
// tie-breaker between equally plausible Cyrillic decodings. It is deliberately
// not a complete language model: the text-quality score remains authoritative,
// and short or non-Russian text can still fall back to the configured default.
var russianLanguageMarkers = []string{
	"при", "рав", "ств", "ени", "ове", "ани", "сво", "лов", "чел", "ого",
	"ния", "ест", "аво", "ние", "льн", "ова", "ать", "или", "его",
	"аци", "лен", "енн", "тво", "сто", "аль", "про", "сти", "пол", "раз",
	"нос", "она", "тел", "ред", "ель", "общ", "под", "ное", "еск", "ели",
	"ече", "для", "ово", "льс", "ции", "ной", "ами", "кон", "сть", "пос",
	"тра", "так", "нал", "дру", "тер", "изн", "соц",
}

func legacyRussianNgramConfidence(text string) float64 {
	text = strings.ToLower(text)
	runeCount := utf8.RuneCountInString(text)
	if runeCount < 3 {
		return 0
	}

	matches := 0
	for _, marker := range russianLanguageMarkers {
		matches += strings.Count(text, marker)
	}
	return float64(matches) / float64(runeCount-2)
}

// A 16 KiB header is enough for text scoring but is unnecessarily expensive
// for a language model. Keep the language tie-breaker bounded and deterministic
// so opening a large file does not spend noticeable time classifying every
// word in the whole header.
func legacyLanguageSample(text string) string {
	const maxRunes = 4096
	runes := []rune(text)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return text
}

func legacyTextScore(data []byte) int {
	score := 0
	for _, r := range string(data) {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			score++
		case stdunicode.IsLetter(r):
			score += 4
		case stdunicode.IsDigit(r):
			score += 2
		case stdunicode.IsSpace(r) || stdunicode.IsPunct(r):
			score++
		case stdunicode.IsControl(r):
			score -= 8
		case stdunicode.IsGraphic(r):
			score++
		default:
			score -= 4
		}

		// Box-drawing characters, non-breaking spaces, and replacement
		// characters are common signs that bytes were decoded with the
		// wrong legacy codepage, even though they are technically graphic.
		if (r >= 0x2500 && r <= 0x259F) || r == '\u00A0' || r == '\uFFFD' {
			score -= 8
		}
		// CP437/CP850 decode several common ANSI punctuation and accented
		// letters as Greek characters. They are valid Unicode letters, but
		// are unlikely in ordinary text and are a useful wrong-codepage hint.
		if stdunicode.In(r, stdunicode.Greek) {
			score -= 8
		}
	}
	return score
}

func GetCodepageDecoderEncoder(cp string) (*encoding.Decoder, *encoding.Encoder) {
	if cp == "" || cp == "65001" {
		return nil, nil
	}
	id, _ := strconv.Atoi(cp)
	if id == 11111 {
		enc := GetSystemANSIEncoding()
		return enc.NewDecoder(), enc.NewEncoder()
	}
	if id == 22222 {
		enc := GetSystemOEMEncoding()
		return enc.NewDecoder(), enc.NewEncoder()
	}
	if cpObj, ok := FindCodepage(id); ok && cpObj.Enc != nil {
		return cpObj.Enc.NewDecoder(), cpObj.Enc.NewEncoder()
	}
	return nil, nil
}

func GetSystemOEMEncoding() encoding.Encoding {
	if oem := getWindowsOEMCP(); oem != nil {
		return oem
	}

	lc := os.Getenv("LC_ALL")
	if lc == "" {
		lc = os.Getenv("LC_CTYPE")
	}
	if lc == "" {
		lc = os.Getenv("LANG")
	}
	if lc == "" || lc == "C" || lc == "POSIX" {
		return charmap.CodePage437
	}

	lcBase := lc
	if idx := strings.IndexByte(lcBase, '.'); idx != -1 {
		lcBase = lcBase[:idx]
	}

	switch lcBase {
	case "ru_RU", "be_BY", "bg_BG", "kk_KZ", "uk_UA", "tt_RU":
		return charmap.CodePage866
	case "cs_CZ", "pl_PL", "hu_HU", "ro_RO", "sk_SK", "hr_HR":
		return charmap.CodePage852
	}
	return charmap.CodePage437
}

func GetSystemANSIEncoding() encoding.Encoding {
	if ansi := getWindowsACP(); ansi != nil {
		return ansi
	}

	lc := os.Getenv("LC_ALL")
	if lc == "" {
		lc = os.Getenv("LC_CTYPE")
	}
	if lc == "" {
		lc = os.Getenv("LANG")
	}
	if lc == "" || lc == "C" || lc == "POSIX" {
		return charmap.Windows1252
	}

	lcBase := lc
	if idx := strings.IndexByte(lcBase, '.'); idx != -1 {
		lcBase = lcBase[:idx]
	}

	switch lcBase {
	case "ru_RU", "be_BY", "bg_BG", "kk_KZ", "uk_UA", "tt_RU":
		return charmap.Windows1251
	case "cs_CZ", "pl_PL", "hu_HU", "ro_RO", "sk_SK", "hr_HR":
		return charmap.Windows1250
	}
	return charmap.Windows1252
}

type MemoryReadAtCloser struct {
	Data []byte
}

func (m *MemoryReadAtCloser) Size() int64 { return int64(len(m.Data)) }
func (m *MemoryReadAtCloser) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	if off >= int64(len(m.Data)) {
		return 0, io.EOF
	}
	n := copy(p, m.Data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (m *MemoryReadAtCloser) Read(ctx context.Context, p []byte) (int, error) {
	return 0, io.EOF
}
func (m *MemoryReadAtCloser) Close() error { return nil }

func GetNextFastSwitchCodepage(current int) int {
	FastSwitchCodepages := []int{65001, 11111, 22222}
	for i, id := range FastSwitchCodepages {
		if id == current {
			nextIdx := (i + 1) % len(FastSwitchCodepages)
			return FastSwitchCodepages[nextIdx]
		}
	}
	return 65001
}
func BuildCodepageMenuItems(currentCpID int, autoDetect bool) ([]vtui.MenuItem, int) {
	var items []vtui.MenuItem
	currIdx := 0

	addHeader := func(title string) {
		items = append(items, vtui.MenuItem{Text: title, Separator: true})
	}

	addCP := func(cp Codepage) {
		if cp.ID == 1251 || cp.ID == 866 {
			return // Exclude duplicate 1251 and 866 from the UI menu
		}
		var text string
		if cp.ID == 11111 || cp.ID == 22222 {
			text = cp.Name // Don't show technical "11111" / "22222" IDs
		} else {
			text = fmt.Sprintf("%5d  %s", cp.ID, cp.Name)
		}

		if cp.ID == currentCpID && !autoDetect {
			text = "√ " + text
			currIdx = len(items)
		} else {
			text = "  " + text
		}
		items = append(items, vtui.MenuItem{
			Text:     text,
			UserData: cp.ID,
		})
	}

	autoText := "  Auto-detect "
	if autoDetect {
		autoText = "√ Auto-detect "
		currIdx = 0
	}
	items = append(items, vtui.MenuItem{
		Text:     autoText,
		UserData: -1,
	})

	addHeader(" System ")
	for _, cp := range AvailableCodepages {
		if cp.ID == 11111 || cp.ID == 22222 {
			addCP(cp)
		}
	}

	addHeader(" Unicode ")
	for _, cp := range AvailableCodepages {
		if cp.ID == 65001 || cp.ID == 1200 || cp.ID == 1201 {
			addCP(cp)
		}
	}

	addHeader(" Other ")
	for _, cp := range AvailableCodepages {
		if cp.ID != 11111 && cp.ID != 22222 && cp.ID != 65001 && cp.ID != 1200 && cp.ID != 1201 {
			addCP(cp)
		}
	}

	return items, currIdx
}
