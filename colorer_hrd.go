package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/unxed/vtui"
)

// ColorerScheme describes a color style ("hrd" file) declared in catalog.xml.
type ColorerScheme struct {
	Name        string
	Description string
	Path        string
}

type colorerRegionStyle struct {
	fore    uint32
	back    uint32
	hasFore bool
	hasBack bool
}

var (
	schemeMu     sync.Mutex
	schemeName   string
	schemeStyles map[string]colorerRegionStyle
	schemeKeys   []string
	schemeMemo   map[string]colorerRegionStyle
)

// ColorerConfigsDir returns the directory the Colorer schemas are unpacked to.
func ColorerConfigsDir() string {
	return filepath.Join(GetF4ConfigDir(), "colorer", "configs")
}

// ListColorerSchemes returns the rgb color styles shipped with the schemas.
// Console styles are skipped: they carry palette indices, not colors.
func ListColorerSchemes() []ColorerScheme {
	return parseColorerCatalog(filepath.Join(ColorerConfigsDir(), "base", "catalog.xml"))
}

// maxColorerCatalogFiles bounds the entity graph, so a catalog referring to
// itself cannot spin the scanner forever.
const maxColorerCatalogFiles = 32

// catalogEntityRe matches an external entity declaration inside the DOCTYPE
// internal subset, e.g.
// <!ENTITY hrd-sets SYSTEM "env:$FAR_HOME/hrd/catalog-console.xml">.
var catalogEntityRe = regexp.MustCompile(`(?is)<!ENTITY\s+(?:%\s+)?[^\s>]+\s+(?:SYSTEM|PUBLIC)\s+((?:"[^"]*"|'[^']*')(?:\s+(?:"[^"]*"|'[^']*'))?)`)

// catalogQuotedRe splits an entity declaration into its quoted parts. For the
// PUBLIC form the system id is the last one.
var catalogQuotedRe = regexp.MustCompile(`"[^"]*"|'[^']*'`)

// catalogEnvRe matches the environment references colorer expands in entity
// paths: $NAME and ${NAME}, plus the Windows %NAME% form.
var catalogEnvRe = regexp.MustCompile(`\$\{([A-Za-z]\w*)\}|\$([A-Za-z]\w*)|%([A-Za-z]\w*)%`)

// parseColorerCatalog collects the rgb color styles reachable from catalog.xml.
// FarColorer catalogs keep the <hrd-sets> block in a separate file pulled in
// with an external XML entity, and encoding/xml never expands those, so the
// entity declarations are followed by hand.
func parseColorerCatalog(catalogPath string) []ColorerScheme {
	var schemes []ColorerScheme
	seenFiles := make(map[string]bool)
	seenNames := make(map[string]bool)

	queue := []string{catalogPath}
	for len(queue) > 0 && len(seenFiles) < maxColorerCatalogFiles {
		path := queue[0]
		queue = queue[1:]

		key := filepath.Clean(path)
		if abs, err := filepath.Abs(path); err == nil {
			key = filepath.Clean(abs)
		}
		if seenFiles[key] {
			continue
		}
		seenFiles[key] = true

		found, entities := scanColorerCatalogFile(path, catalogPath)
		for _, scheme := range found {
			lower := strings.ToLower(scheme.Name)
			if seenNames[lower] {
				continue
			}
			seenNames[lower] = true
			schemes = append(schemes, scheme)
		}
		for _, entity := range entities {
			if resolved := resolveColorerCatalogEntity(entity, path); resolved != "" {
				queue = append(queue, resolved)
			}
		}
	}

	sort.Slice(schemes, func(i, j int) bool {
		return strings.ToLower(schemes[i].Name) < strings.ToLower(schemes[j].Name)
	})
	return schemes
}

// scanColorerCatalogFile reads a single catalog file and returns the rgb
// styles it declares together with the system ids of the external entities it
// references. Style locations are resolved against the main catalog, the way
// the colorer engine itself does it.
func scanColorerCatalogFile(path, catalogPath string) ([]ColorerScheme, []string) {
	f, err := os.Open(path)
	if err != nil {
		vtui.DebugLog("COLORER: Cannot open catalog file %q: %v", path, err)
		return nil, nil
	}
	defer f.Close()

	var schemes []ColorerScheme
	var entities []string
	var current *ColorerScheme

	dec := xml.NewDecoder(f)
	dec.Strict = false
	for {
		tok, tErr := dec.Token()
		if tErr != nil {
			break
		}
		switch el := tok.(type) {
		case xml.Directive:
			entities = append(entities, colorerCatalogEntities(string(el))...)
		case xml.StartElement:
			switch strings.ToLower(el.Name.Local) {
			case "hrd":
				current = nil
				if !strings.EqualFold(xmlAttr(el, "class"), "rgb") {
					continue
				}
				current = &ColorerScheme{
					Name:        xmlAttr(el, "name"),
					Description: xmlAttr(el, "description"),
				}
			case "location":
				if current != nil && current.Path == "" {
					link := xmlAttr(el, "link")
					if link != "" {
						current.Path = filepath.Join(filepath.Dir(catalogPath), filepath.FromSlash(link))
					}
				}
			}
		case xml.EndElement:
			if strings.EqualFold(el.Name.Local, "hrd") {
				if current != nil && current.Name != "" && current.Path != "" {
					schemes = append(schemes, *current)
				}
				current = nil
			}
		}
	}

	return schemes, entities
}

// colorerCatalogEntities pulls the system ids out of the entity declarations
// of a DOCTYPE directive. Internal entities carry no path and are skipped.
func colorerCatalogEntities(directive string) []string {
	var paths []string
	for _, decl := range catalogEntityRe.FindAllStringSubmatch(directive, -1) {
		quoted := catalogQuotedRe.FindAllString(decl[1], -1)
		if len(quoted) == 0 {
			continue
		}
		value := quoted[len(quoted)-1]
		paths = append(paths, value[1:len(value)-1])
	}
	return paths
}

// resolveColorerCatalogEntity turns an entity system id into a readable path.
// FarColorer writes them as "env:$FAR_HOME/hrd/catalog-console.xml": the
// variable is expanded when the environment defines it, and what is left is
// looked up next to the file that declared the entity otherwise.
func resolveColorerCatalogEntity(systemID, fromFile string) string {
	text := strings.TrimSpace(systemID)
	text = strings.TrimPrefix(text, "env:")
	text = strings.TrimPrefix(text, "file://")
	if text == "" {
		return ""
	}
	text = filepath.FromSlash(expandColorerCatalogEnv(text))

	if isColorerCatalogFile(text) {
		return text
	}
	relative := strings.TrimLeft(text, `/\`)
	if candidate := filepath.Join(filepath.Dir(fromFile), relative); isColorerCatalogFile(candidate) {
		return candidate
	}

	vtui.DebugLog("COLORER: Catalog entity %q could not be resolved", systemID)
	return ""
}

// expandColorerCatalogEnv replaces the environment references of an entity
// path. An undefined variable expands to nothing, which leaves a path that is
// then looked up relative to the declaring file.
func expandColorerCatalogEnv(text string) string {
	return catalogEnvRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := catalogEnvRe.FindStringSubmatch(match)
		for _, name := range groups[1:] {
			if name != "" {
				return os.Getenv(name)
			}
		}
		return match
	})
}

func isColorerCatalogFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadColorerScheme(path string) map[string]colorerRegionStyle {
	f, err := os.Open(path)
	if err != nil {
		vtui.DebugLog("COLORER: Cannot open color style %q: %v", path, err)
		return nil
	}
	defer f.Close()

	styles := make(map[string]colorerRegionStyle)
	dec := xml.NewDecoder(f)
	dec.Strict = false
	for {
		tok, tErr := dec.Token()
		if tErr != nil {
			break
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		name := xmlAttr(el, "name")
		if name == "" {
			continue
		}
		var style colorerRegionStyle
		style.fore, style.hasFore = parseColorerColor(xmlAttr(el, "fore"))
		style.back, style.hasBack = parseColorerColor(xmlAttr(el, "back"))
		if style.hasFore || style.hasBack {
			styles[strings.ToLower(name)] = style
		}
	}
	return styles
}

// parseColorerColor understands the "#RRGGBB", "0xRRGGBB" and "RRGGBB" forms
// used by rgb color styles. Console palette indices are rejected.
func parseColorerColor(value string) (uint32, bool) {
	text := strings.TrimSpace(value)
	text = strings.TrimPrefix(text, "#")
	if len(text) > 2 && (strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X")) {
		text = text[2:]
	}
	if len(text) == 8 {
		// An alpha channel may be prepended; the color itself is the low part.
		text = text[2:]
	}
	if len(text) != 6 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(text, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

// SetColorerScheme activates a color style by name. An empty or unknown name
// switches back to the built-in color map.
func SetColorerScheme(name string) {
	schemeMu.Lock()
	unchanged := name == schemeName
	schemeMu.Unlock()
	if unchanged {
		return
	}

	var styles map[string]colorerRegionStyle
	if name != "" {
		for _, scheme := range ListColorerSchemes() {
			if strings.EqualFold(scheme.Name, name) {
				styles = loadColorerScheme(scheme.Path)
				break
			}
		}
	}

	keys := make([]string, 0, len(styles))
	for key := range styles {
		keys = append(keys, key)
	}
	sortColorerKeys(keys)

	schemeMu.Lock()
	schemeName = name
	schemeStyles = styles
	schemeKeys = keys
	schemeMemo = make(map[string]colorerRegionStyle)
	schemeMu.Unlock()

	vtui.DebugLog("COLORER: Color style %q activated, %d regions defined", name, len(styles))
}

// colorerSchemeStyle resolves a region name through the active color style,
// using the same exact/prefix/substring rules as the built-in color map.
func colorerSchemeStyle(name string) (colorerRegionStyle, bool) {
	nameLower := strings.ToLower(name)

	schemeMu.Lock()
	defer schemeMu.Unlock()
	if schemeStyles == nil {
		return colorerRegionStyle{}, false
	}
	if cached, hit := schemeMemo[nameLower]; hit {
		return cached, cached.hasFore || cached.hasBack
	}

	style, found := schemeStyles[nameLower]
	if !found {
		for _, key := range schemeKeys {
			if strings.HasPrefix(nameLower, key) {
				style, found = schemeStyles[key], true
				break
			}
		}
	}
	if !found {
		for _, key := range schemeKeys {
			if strings.Contains(nameLower, key) {
				style, found = schemeStyles[key], true
				break
			}
		}
	}
	if !found {
		style = colorerRegionStyle{}
	}

	schemeMemo[nameLower] = style
	return style, found
}

// sortColorerKeys orders keys from the longest to the shortest one, so that
// the most specific key always wins and the result never depends on Go's
// random map iteration order.
func sortColorerKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
}

func xmlAttr(el xml.StartElement, name string) string {
	for _, attr := range el.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}
