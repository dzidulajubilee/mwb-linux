package group

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// StorePath is where learned member addresses are kept between runs:
// $XDG_STATE_HOME/mwb/members.json, or ~/.local/state/mwb/members.json.
func StorePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "mwb", "members.json")
}

// loadMembers reads the learned addresses; a missing or unreadable file is
// an empty group, since the members are learned again on connect.
func loadMembers(path string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return out
	}
	for n, addr := range m {
		if validName(n) && addr != "" && len(out) < MaxMachines {
			out[n] = addr
		}
	}
	return out
}

// saveMembers writes the learned addresses atomically, owner-only.
func saveMembers(path string, members map[string]string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(members, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".members-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
