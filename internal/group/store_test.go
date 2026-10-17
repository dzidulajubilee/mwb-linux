package group

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMembersStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mwb", "members.json")
	if got := loadMembers(path); len(got) != 0 {
		t.Fatalf("missing file gave %v", got)
	}
	want := map[string]string{"desk": "10.0.0.7", "laptop": "fe80::1%eth0"}
	if err := saveMembers(path, want); err != nil {
		t.Fatal(err)
	}
	got := loadMembers(path)
	if len(got) != 2 || got["desk"] != "10.0.0.7" || got["laptop"] != "fe80::1%eth0" {
		t.Fatalf("got %v", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", info.Mode().Perm(), err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadMembers(path); len(got) != 0 {
		t.Fatalf("corrupt file gave %v", got)
	}
}
