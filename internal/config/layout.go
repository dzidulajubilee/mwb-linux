// internal/config/layout.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// SaveLayout rewrites layout and layout_changed in the config file at path,
// keeping every other line, comment and permission as it is. changed <= 0
// removes layout_changed.
func SaveLayout(path string, layout []string, changed int64) error {
	if len(layout) == 0 {
		return fmt.Errorf("layout is empty")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return err
	}
	text := setLayoutLines(string(data), layout, changed)

	var check Config
	if _, err := toml.Decode(text, &check); err != nil {
		return fmt.Errorf("updated config would not parse: %w", err)
	}
	if !slices.Equal(check.Layout, layout) || (changed > 0 && check.LayoutChanged != changed) {
		return fmt.Errorf("layout did not survive the update")
	}
	return writeConfigAtomic(real, text)
}

func setLayoutLines(text string, layout []string, changed int64) string {
	quoted := make([]string, len(layout))
	for i, name := range layout {
		quoted[i] = strconv.Quote(name)
	}
	want := map[string]string{"layout": "layout = [" + strings.Join(quoted, ", ") + "]\n"}
	if changed > 0 {
		want["layout_changed"] = "layout_changed = " + strconv.FormatInt(changed, 10) + "\n"
	}
	return setKeyLines(text, []string{"layout", "layout_changed"}, want)
}
