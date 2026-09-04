package collector

import (
	"testing"
)

func TestCollectSoftwareLive(t *testing.T) {
	sw, err := collectSoftwareWindows()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(sw) == 0 {
		t.Fatal("expected non-empty software list")
	}
}
