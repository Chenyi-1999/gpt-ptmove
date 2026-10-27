package extract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
)

func TestImportedProfileUsesNativeSourceMapping(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTNEXUS_BASE_DIR", dir)
	t.Setenv("PTNEXUS_DATA_DIR", filepath.Join(dir, "data"))
	if e := os.MkdirAll(filepath.Join(dir, "configs"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "configs", "sourcefixture.yaml"), []byte("source_parsers:\n  source_params:\n    type:\n      source_key: FixtureCategory\n  standard_keys:\n    type:\n      FixtureMovie: category.movie\n"), 0600); e != nil {
		t.Fatal(e)
	}
	siteprofile.RegisterMapping("customsourcefixture", "sourcefixture")
	if got := loadSiteSourceFieldKey("customsourcefixture", "type"); got != "FixtureCategory" {
		t.Fatalf("source field overlay ignored: %q", got)
	}
	table := loadSiteStandardFieldMappingTable(dir, "customsourcefixture", "type")
	if len(table.entries) == 0 {
		t.Fatal("source standard mapping overlay ignored")
	}
}
