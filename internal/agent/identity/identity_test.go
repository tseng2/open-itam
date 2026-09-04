package identity

import (
	"errors"
	"strings"
	"testing"
)

var testProbe = ProbeFuncs{
	BaseboardSerial: func() (string, error) { return "MB-SN-123", nil },
	FirstMAC:        func() (string, error) { return "AA:BB:CC:DD:EE:01", nil },
}

func TestDeviceIDDeterministic(t *testing.T) {
	id1, err := DeviceID(testProbe)
	if err != nil {
		t.Fatalf("device id: %v", err)
	}
	id2, err := DeviceID(testProbe)
	if err != nil {
		t.Fatalf("device id again: %v", err)
	}
	if id1 != id2 {
		t.Fatal("device id must be deterministic")
	}
	if len(id1) != 16 {
		t.Fatalf("expected 16 chars, got %d (%q)", len(id1), id1)
	}
}

func TestDeviceIDDiffersOnInput(t *testing.T) {
	a, _ := DeviceID(testProbe)
	b, _ := DeviceID(ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "MB-SN-999", nil },
		FirstMAC:        func() (string, error) { return "AA:BB:CC:DD:EE:01", nil },
	})
	if a == b {
		t.Fatal("different baseboard serial must yield different id")
	}
}

func TestDeviceIDMissingSources(t *testing.T) {
	empty := ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "", nil },
		FirstMAC:        func() (string, error) { return "", nil },
	}
	if _, err := DeviceID(empty); err == nil {
		t.Fatal("expected error when both sources empty")
	}
}

func TestDeviceIDToleratesProbeErrors(t *testing.T) {
	p := ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "", errors.New("wmi down") },
		FirstMAC:        func() (string, error) { return "aa:bb:cc:dd:ee:01", nil },
	}
	id, err := DeviceID(p)
	if err != nil {
		t.Fatalf("probe error on serial must fall back to MAC: %v", err)
	}
	macOnly, _ := DeviceID(ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "", nil },
		FirstMAC:        func() (string, error) { return "aa:bb:cc:dd:ee:01", nil },
	})
	if id != macOnly {
		t.Fatal("error case must produce same id as empty case")
	}
}

func TestNormalizeMAC(t *testing.T) {
	got := normalizeMAC("aa-bb-cc-dd-ee-ff")
	if got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("normalize failed: %q", got)
	}
	if normalizeMAC("") != "" {
		t.Fatal("empty mac must stay empty")
	}
}

func TestFilterGarbageSerial(t *testing.T) {
	for _, bad := range []string{"Default string", "To be filled by O.E.M.", "System Serial Number", "None", "  "} {
		if !isGarbageSerial(bad) {
			t.Fatalf("%q should be garbage", bad)
		}
	}
	if isGarbageSerial("7XKQ1P3") {
		t.Fatal("real serial wrongly filtered")
	}
}

func TestSerialWithGarbageFallback(t *testing.T) {
	p := ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "Default string", nil },
		FirstMAC:        func() (string, error) { return "AA:BB:CC:DD:EE:01", nil },
	}
	id, err := DeviceID(p)
	if err != nil {
		t.Fatalf("should succeed with MAC only: %v", err)
	}
	macOnly, _ := DeviceID(ProbeFuncs{
		BaseboardSerial: func() (string, error) { return "", nil },
		FirstMAC:        func() (string, error) { return "AA:BB:CC:DD:EE:01", nil },
	})
	if id != macOnly {
		t.Fatal("garbage serial must be treated as empty")
	}
	if strings.Contains(strings.ToLower(id), "default") {
		t.Fatal("garbage leaked into id")
	}
}
