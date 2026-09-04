package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Spool struct {
	dir string
}

func NewSpool(dir string) *Spool {
	return &Spool{dir: dir}
}

func (s *Spool) Enqueue(payload []byte) (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%d.json", time.Now().UnixNano(), os.Getpid())
	path := filepath.Join(s.dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return name, nil
}

func (s *Spool) Pending() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (s *Spool) Read(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, name))
}

func (s *Spool) Delete(name string) error {
	err := os.Remove(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Spool) Stats() (count int, totalBytes int64, err error) {
	names, err := s.Pending()
	if err != nil {
		return 0, 0, err
	}
	for _, n := range names {
		info, err := os.Stat(filepath.Join(s.dir, n))
		if err != nil {
			continue
		}
		totalBytes += info.Size()
	}
	return len(names), totalBytes, nil
}

type Backoff struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

func NewBackoff(base, max time.Duration) *Backoff {
	return &Backoff{base: base, max: max}
}

func (b *Backoff) Next() time.Duration {
	if b.current == 0 {
		b.current = b.base
	} else {
		b.current *= 2
		if b.current > b.max {
			b.current = b.max
		}
	}
	return b.current
}

func (b *Backoff) Reset() {
	b.current = 0
}
