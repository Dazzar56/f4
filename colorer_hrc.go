package main

import (
	"encoding/xml"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/unxed/vtui"
)

// maxColorerHrcFiles bounds the scan of the schema tree, so that a folder the
// user pointed at by mistake cannot keep the scanner busy forever.
const maxColorerHrcFiles = 8192

// colorerHrcType collects the region declarations of a single <type> block.
// Parents are kept as written and resolved afterwards, since an unqualified
// one may refer to a region declared in another file.
type colorerHrcType struct {
	name    string
	imports []string
	regions map[string]string
}

var (
	hrcMu      sync.Mutex
	hrcDir     string
	hrcParents map[string]string
	hrcLoading bool
)

// ColorerRegionParents maps a fully qualified region name to its parent, both
// lower cased. It returns nil while the schemas have not been scanned, and
// also when the scan found nothing, so that the callers fall back to their
// approximations instead of painting everything with the base color.
func ColorerRegionParents() map[string]string {
	hrcMu.Lock()
	defer hrcMu.Unlock()
	if len(hrcParents) == 0 {
		return nil
	}
	return hrcParents
}

// StartColorerRegionScan builds the region graph of dir in the background. The
// scan reads the whole hrc tree, which is far too slow to do while a frame is
// being drawn.
func StartColorerRegionScan(dir string) {
	hrcMu.Lock()
	if hrcLoading || (hrcParents != nil && hrcDir == dir) {
		hrcMu.Unlock()
		return
	}
	hrcLoading = true
	hrcMu.Unlock()

	go func() {
		parents := scanColorerRegions(dir)

		hrcMu.Lock()
		hrcParents = parents
		hrcDir = dir
		hrcLoading = false
		hrcMu.Unlock()

		vtui.DebugLog("COLORER: Region graph of %q holds %d parents", dir, len(parents))
		InvalidateColorerRegionCache()
		vtui.FrameManager.PostTask(func() {
			vtui.FrameManager.Redraw()
		})
	}()
}

// ResetColorerRegions drops the graph, so that the next opened file scans the
// schemas again.
func ResetColorerRegions() {
	hrcMu.Lock()
	hrcParents = nil
	hrcDir = ""
	hrcMu.Unlock()
}

// scanColorerRegions walks the schema tree and returns the region parents it
// declares.
func scanColorerRegions(dir string) map[string]string {
	var types []*colorerHrcType
	scanned := 0

	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".hrc") {
			return nil
		}
		if scanned >= maxColorerHrcFiles {
			return fs.SkipAll
		}
		scanned++
		types = append(types, scanColorerHrcFile(path)...)
		return nil
	}); err != nil {
		vtui.DebugLog("COLORER: Cannot scan the schemas in %q: %v", dir, err)
	}

	// A parent can only be resolved once every region of every type is known,
	// so the own names are collected first.
	owned := make(map[string]bool)
	for _, ht := range types {
		for name := range ht.regions {
			owned[qualifyColorerRegion(ht.name, name)] = true
		}
	}

	parents := make(map[string]string)
	for _, ht := range types {
		for name, parent := range ht.regions {
			qualified := qualifyColorerRegion(ht.name, name)
			resolved := resolveColorerRegionParent(ht, parent, owned)
			if resolved != "" && resolved != qualified {
				parents[qualified] = resolved
			}
		}
	}
	return parents
}

// qualifyColorerRegion prefixes a region name with the type that declares it,
// the way colorer's qualifyOwnName does.
func qualifyColorerRegion(typeName, name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(lower, ":") {
		return lower
	}
	return strings.ToLower(strings.TrimSpace(typeName)) + ":" + lower
}

// resolveColorerRegionParent turns the parent attribute of a region into a
// fully qualified name. A qualified one is taken as it is; an unqualified one
// is looked for in the declaring type first and in its imports afterwards,
// which is the order colorer's qualifyForeignName uses.
func resolveColorerRegionParent(ht *colorerHrcType, parent string, owned map[string]bool) string {
	lower := strings.ToLower(strings.TrimSpace(parent))
	if lower == "" {
		return ""
	}
	if strings.Contains(lower, ":") {
		return lower
	}

	candidates := make([]string, 0, len(ht.imports)+1)
	candidates = append(candidates, ht.name)
	candidates = append(candidates, ht.imports...)
	for _, candidate := range candidates {
		qualified := strings.ToLower(strings.TrimSpace(candidate)) + ":" + lower
		if owned[qualified] {
			return qualified
		}
	}
	return ""
}

// scanColorerHrcFile reads the <type> blocks of a single schema file.
func scanColorerHrcFile(path string) []*colorerHrcType {
	f, err := os.Open(path)
	if err != nil {
		vtui.DebugLog("COLORER: Cannot open the schema %q: %v", path, err)
		return nil
	}
	defer f.Close()

	var types []*colorerHrcType
	var current *colorerHrcType
	entitiesMap := make(map[string]string)

	dec := xml.NewDecoder(f)
	dec.Strict = false
	dec.Entity = entitiesMap
	dec.CharsetReader = colorerCharsetReader
	for {
		tok, tErr := dec.Token()
		if tErr != nil {
			break
		}
		switch el := tok.(type) {
		case xml.Directive:
			parseDirectiveEntities(string(el), entitiesMap)
		case xml.StartElement:
			switch strings.ToLower(el.Name.Local) {
			case "type":
				current = nil
				name := xmlAttr(el, "name")
				if name == "" {
					continue
				}
				current = &colorerHrcType{name: name, regions: make(map[string]string)}
				types = append(types, current)
			case "import":
				if current == nil {
					continue
				}
				if imported := xmlAttr(el, "type"); imported != "" {
					current.imports = append(current.imports, imported)
				}
			case "region":
				if current == nil {
					continue
				}
				name := xmlAttr(el, "name")
				if name == "" {
					continue
				}
				// The first declaration wins, the way colorer treats a
				// duplicate region.
				if _, seen := current.regions[name]; !seen {
					current.regions[name] = xmlAttr(el, "parent")
				}
			}
		case xml.EndElement:
			if strings.EqualFold(el.Name.Local, "type") {
				current = nil
			}
		}
	}

	return types
}
