package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/vtui"
)

func writeColorerCatalogFixture(t *testing.T) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(filepath.Join(base, "hrd", "rgb"), 0755); err != nil {
		t.Fatalf("Cannot create the fixture directory: %v", err)
	}

	catalog := `<?xml version="1.0" encoding="UTF-8"?>
<catalog>
  <hrd-sets>
    <hrd class="console" name="console" description="Console style">
      <location link="hrd/console/console.hrd"/>
    </hrd>
    <hrd class="rgb" name="test" description="Test style">
      <location link="hrd/rgb/test.hrd"/>
    </hrd>
  </hrd-sets>
</catalog>`
	hrd := `<?xml version="1.0" encoding="UTF-8"?>
<hrd>
  <assign name="def:Comment" fore="#123456"/>
  <assign name="def:String" fore="#00FF00" back="#101010"/>
  <assign name="def:Ignored"/>
</hrd>`

	catalogPath := filepath.Join(base, "catalog.xml")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0644); err != nil {
		t.Fatalf("Cannot write the catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "hrd", "rgb", "test.hrd"), []byte(hrd), 0644); err != nil {
		t.Fatalf("Cannot write the color style: %v", err)
	}
	return catalogPath
}

// writeColorerEntityCatalogFixture builds a FarColorer-shaped catalog: the
// main file declares the hrc locations only and pulls the hrd sets in with an
// external entity. The entity path is returned separately, so a test can point
// the environment variable at it or leave it undefined.
func writeColorerEntityCatalogFixture(t *testing.T, entityLink string) (string, string) {
	t.Helper()

	base := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(filepath.Join(base, "hrd", "rgb"), 0755); err != nil {
		t.Fatalf("Cannot create the fixture directory: %v", err)
	}

	catalog := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE catalog [
  <!ENTITY hrd-sets SYSTEM "` + entityLink + `">
]>
<catalog>
  <hrc-sets>
    <location link="hrc/proto/base.xml"/>
  </hrc-sets>
  &hrd-sets;
</catalog>`
	hrdSets := `<hrd-sets>
  <hrd class="console" name="console" description="Console style">
    <location link="hrd/console/console.hrd"/>
  </hrd>
  <hrd class="rgb" name="zebra" description="Zebra style">
    <location link="hrd/rgb/zebra.hrd"/>
  </hrd>
  <hrd class="rgb" name="amber" description="Amber style">
    <location link="hrd/rgb/amber.hrd"/>
  </hrd>
</hrd-sets>`

	catalogPath := filepath.Join(base, "catalog.xml")
	entityPath := filepath.Join(base, "hrd", "catalog-rgb.xml")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0644); err != nil {
		t.Fatalf("Cannot write the catalog: %v", err)
	}
	if err := os.WriteFile(entityPath, []byte(hrdSets), 0644); err != nil {
		t.Fatalf("Cannot write the hrd sets: %v", err)
	}
	return catalogPath, entityPath
}

func assertEntityCatalogSchemes(t *testing.T, catalogPath string) {
	t.Helper()

	schemes := parseColorerCatalog(catalogPath)
	if len(schemes) != 2 {
		t.Fatalf("Expected two rgb styles behind the entity, got %v", schemes)
	}
	if schemes[0].Name != "amber" || schemes[1].Name != "zebra" {
		t.Fatalf("Expected the styles sorted by name, got %v", schemes)
	}

	wantPath := filepath.Join(filepath.Dir(catalogPath), "hrd", "rgb", "amber.hrd")
	if schemes[0].Path != wantPath {
		t.Errorf("Expected the location resolved against the catalog as %q, got %q", wantPath, schemes[0].Path)
	}
}

func TestColorer_ParseCatalogFollowsEntity(t *testing.T) {
	// The variable is undefined, so the remainder of the path has to be
	// looked up next to the catalog itself.
	catalogPath, _ := writeColorerEntityCatalogFixture(t, "env:$F4_COLORER_TEST_HOME/hrd/catalog-rgb.xml")
	assertEntityCatalogSchemes(t, catalogPath)
}

func TestColorer_ParseCatalogExpandsEntityEnv(t *testing.T) {
	catalogPath, entityPath := writeColorerEntityCatalogFixture(t, "env:$F4_COLORER_TEST_HOME/catalog-rgb.xml")
	t.Setenv("F4_COLORER_TEST_HOME", filepath.Dir(entityPath))
	assertEntityCatalogSchemes(t, catalogPath)
}

func TestColorer_ParseCatalogSurvivesBrokenEntity(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatalf("Cannot create the fixture directory: %v", err)
	}

	catalog := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE catalog [
  <!ENTITY missing SYSTEM "env:$F4_COLORER_TEST_HOME/nowhere.xml">
]>
<catalog>
  <hrd-sets>
    &missing;
    <hrd class="rgb" name="test" description="Test style">
      <location link="hrd/rgb/test.hrd"/>
    </hrd>
  </hrd-sets>
</catalog>`
	catalogPath := filepath.Join(base, "catalog.xml")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0644); err != nil {
		t.Fatalf("Cannot write the catalog: %v", err)
	}

	schemes := parseColorerCatalog(catalogPath)
	if len(schemes) != 1 || schemes[0].Name != "test" {
		t.Fatalf("An unresolvable entity must not hide the inline styles, got %v", schemes)
	}
}

func TestColorer_ParseCatalogAndScheme(t *testing.T) {
	inlinePath := writeColorerCatalogFixture(t)

	schemes := parseColorerCatalog(inlinePath)
	if len(schemes) != 1 || schemes[0].Name != "test" {
		t.Fatalf("Expected exactly one rgb style, got %v", schemes)
	}

	styles := loadColorerScheme(schemes[0].Path)
	if len(styles) != 2 {
		t.Fatalf("Expected two defined regions, got %d", len(styles))
	}
	if style := styles["def:comment"]; !style.hasFore || style.fore != 0x123456 || style.hasBack {
		t.Errorf("Unexpected style for def:Comment: %+v", style)
	}
	if style := styles["def:string"]; !style.hasBack || style.back != 0x101010 {
		t.Errorf("Unexpected style for def:String: %+v", style)
	}
}

func TestColorer_ParseColorerColor(t *testing.T) {
	cases := []struct {
		value string
		want  uint32
		ok    bool
	}{
		{"#AABBCC", 0xAABBCC, true},
		{"0xAABBCC", 0xAABBCC, true},
		{"AABBCC", 0xAABBCC, true},
		{"#FFAABBCC", 0xAABBCC, true},
		{"0x7", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := parseColorerColor(c.value)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Value %q parsed to %06X, %v; expected %06X, %v", c.value, got, ok, c.want, c.ok)
		}
	}
}

func TestColorer_SchemeOverridesBuiltinColors(t *testing.T) {
	schemeMu.Lock()
	oldName, oldStyles, oldKeys, oldMemo := schemeName, schemeStyles, schemeKeys, schemeMemo
	schemeName = "test"
	schemeStyles = map[string]colorerRegionStyle{
		"def:comment": {fore: 0x123456, hasFore: true},
	}
	schemeKeys = []string{"def:comment"}
	schemeMemo = make(map[string]colorerRegionStyle)
	schemeMu.Unlock()

	defer func() {
		schemeMu.Lock()
		schemeName, schemeStyles, schemeKeys, schemeMemo = oldName, oldStyles, oldKeys, oldMemo
		schemeMu.Unlock()
	}()

	if got := vtui.GetRGBFore(getColorerAttr("def:CommentContent", 0)); got != 0x123456 {
		t.Errorf("Expected the color style to win, got %06X", got)
	}
	if got := getColorerAttr("unknown_region", 0); got != 0 {
		t.Errorf("Expected the base attribute for an unknown region, got %d", got)
	}
}
