// internal/status/status.go

// Package status publishes the running client's connection state to a small
// file, so a desktop front end (mwb-gui) can show it without opening a socket
// or parsing logs.
//
// The file lives under $XDG_RUNTIME_DIR, which systemd creates per user with
// mode 0700 on a tmpfs, so it never outlives the login session and other
// accounts cannot read it. If XDG_RUNTIME_DIR is unset nothing is written:
// falling back to a shared directory such as /tmp would let another local user
// plant or read the file.
//
// The file holds no secrets: never the security key.
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// States written to the file.
const (
	StateConnecting = "connecting"
	StateConnected  = "connected"
)

// Status is the JSON document mwb-gui reads.
type Status struct {
	State         string    `json:"state"`
	Peer          string    `json:"peer"`
	Host          string    `json:"host"`
	Remote        string    `json:"remote,omitempty"`
	Outbound      bool      `json:"outbound,omitempty"`
	Bidirectional bool      `json:"bidirectional"`
	Edge          string    `json:"edge"`
	Clipboard     bool      `json:"clipboard"`
	PID           int       `json:"pid"`
	Version       string    `json:"version"`
	Since         time.Time `json:"since"`
}

// Path returns the status file location, or "" when XDG_RUNTIME_DIR is unset.
func Path() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Join(dir, "mwb", "status.json")
}

// Writer publishes updates to one status file. The zero value is not usable;
// use NewWriter. A Writer with an empty path discards everything.
type Writer struct {
	mu   sync.Mutex
	path string
	base Status
}

// NewWriter returns a writer for path, using base for the fields that do not
// change during the process lifetime. An empty path yields a no-op writer.
func NewWriter(path string, base Status) *Writer {
	return &Writer{path: path, base: base}
}

// Connecting records that the client is waiting for its peer.
func (w *Writer) Connecting() error {
	return w.write(func(s *Status) {
		s.State = StateConnecting
		s.Remote = ""
		s.Outbound = false
	})
}

// Connected records an established control connection.
func (w *Writer) Connected(remote string, outbound bool) error {
	return w.write(func(s *Status) {
		s.State = StateConnected
		s.Remote = remote
		s.Outbound = outbound
	})
}

// Remove deletes the status file on clean shutdown. A crash leaves it behind;
// readers detect that through the recorded PID.
func (w *Writer) Remove() {
	if w == nil || w.path == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = os.Remove(w.path)
}

func (w *Writer) write(update func(*Status)) error {
	if w == nil || w.path == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	s := w.base
	update(&s)
	s.Since = time.Now().UTC().Truncate(time.Second)
	w.base = s

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create status dir: %w", err)
	}
	// Write then rename, so a reader never sees a half-written file.
	tmp, err := os.CreateTemp(dir, ".status-*.json")
	if err != nil {
		return fmt.Errorf("create status file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write status file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close status file: %w", err)
	}
	if err := os.Rename(tmpName, w.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("publish status file: %w", err)
	}
	return nil
}
