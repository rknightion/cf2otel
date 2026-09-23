package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CheckpointStore interface {
	Get(string) (time.Time, bool)
	Set(string, time.Time) error
}
type FileStore struct {
	mu     sync.Mutex
	path   string
	values map[string]time.Time
}

func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, values: map[string]time.Time{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.values); err != nil {
		return nil, fmt.Errorf("checkpoint decode: %w", err)
	}
	return s, nil
}
func (s *FileStore) Get(name string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.values[name]
	return t, ok
}
func (s *FileStore) Set(name string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]time.Time, len(s.values)+1)
	for k, v := range s.values {
		next[k] = v
	}
	next[name] = t
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".checkpoint-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	s.values = next
	return nil
}
