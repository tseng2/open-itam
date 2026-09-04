package collector

import "testing"

func TestPSBasicLive(t *testing.T) {
	var out struct {
		Name string `json:"name"`
	}
	if err := runPSJSON(`@{name='ok'} | ConvertTo-Json -Compress`, &out); err != nil {
		t.Fatalf("basic ps: %v", err)
	}
	if out.Name != "ok" {
		t.Fatalf("unexpected: %+v", out)
	}
}
