package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolEnqueueDrainDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)

	payload := []byte(`{"device_id":"d1"}`)
	name, err := s.Enqueue(payload)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if !strings.HasSuffix(name, ".json") {
		t.Fatalf("spool file must be .json: %s", name)
	}

	items, err := s.Pending()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(items) != 1 || items[0] != name {
		t.Fatalf("expected 1 pending item %s, got %v", name, items)
	}

	got, err := s.Read(name)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatal("payload corrupted in spool")
	}

	if err := s.Delete(name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ = s.Pending()
	if len(items) != 0 {
		t.Fatalf("spool not empty after delete: %v", items)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatal("file must be gone after delete (本地零残留)")
	}
}

func TestSpoolFIFOOrder(t *testing.T) {
	s := NewSpool(t.TempDir())
	for i := 0; i < 3; i++ {
		if _, err := s.Enqueue([]byte{byte('0' + i)}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	items, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	for i, name := range items {
		data, _ := s.Read(name)
		if data[0] != byte('0'+i) {
			t.Fatalf("FIFO violated at %d: got %q", i, data)
		}
	}
}

func TestSpoolIgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)
	os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "partial.tmp"), []byte("x"), 0o600)
	s.Enqueue([]byte("{}"))
	items, _ := s.Pending()
	if len(items) != 1 {
		t.Fatalf("must ignore non-json files, got %v", items)
	}
}

func TestPendingOnMissingDir(t *testing.T) {
	s := NewSpool(t.TempDir() + "/nonexistent/sub")
	items, err := s.Pending()
	if err != nil || items != nil {
		t.Fatalf("missing dir must yield empty list: %v %v", items, err)
	}
	if err := s.Delete("nope.json"); err != nil {
		t.Fatalf("delete of missing file must be idempotent: %v", err)
	}
	if _, err := s.Read("nope.json"); err == nil {
		t.Fatal("read of missing file must error")
	}
}

func TestBackoff(t *testing.T) {
	b := NewBackoff(time.Second, 60*time.Second)
	if d := b.Next(); d != 1*time.Second {
		t.Fatalf("first backoff %v", d)
	}
	if d := b.Next(); d != 2*time.Second {
		t.Fatalf("second backoff %v", d)
	}
	for i := 0; i < 20; i++ {
		b.Next()
	}
	if d := b.Next(); d != 60*time.Second {
		t.Fatalf("must cap at max: %v", d)
	}
	b.Reset()
	if d := b.Next(); d != 1*time.Second {
		t.Fatalf("reset failed: %v", d)
	}
}

func TestSpoolCountAndSize(t *testing.T) {
	dir := t.TempDir()
	s := NewSpool(dir)
	s.Enqueue([]byte("12345"))
	s.Enqueue([]byte("67890"))
	n, bytes, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || bytes != 10 {
		t.Fatalf("stats wrong: n=%d bytes=%d", n, bytes)
	}
}
