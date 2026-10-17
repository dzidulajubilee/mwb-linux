// internal/config/layout_test.go
package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSaveLayoutKeepsFileUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# settings\nhost = \"10.0.0.5\"\nkey = \"abcdefghijklmnop\"\npeer = \"linux\"\nlayout = [\"a\"]  # old\n\n[extra]\nlayout = \"keep\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"left one", "mid\"dle", "right"}
	if err := SaveLayout(path, want, 1700000000); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Layout, want) || cfg.LayoutChanged != 1700000000 || cfg.Host != "10.0.0.5" {
		t.Fatalf("after SaveLayout: %+v", cfg)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(data), "[extra]\nlayout = \"keep\"\n") {
		t.Fatalf("the table's own layout key changed:\n%s", data)
	}
	if err := SaveLayout(path, nil, 0); err == nil {
		t.Fatal("an empty layout was saved")
	}
}
