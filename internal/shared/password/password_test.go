package password

import (
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("admin123")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify("admin123", hash) {
		t.Fatal("same password must verify")
	}
	if Verify("wrong123", hash) {
		t.Fatal("wrong password must not verify")
	}
}

func TestDifferentSalts(t *testing.T) {
	h1, _ := Hash("abcdefgh")
	h2, _ := Hash("abcdefgh")
	if h1 == h2 {
		t.Fatal("salt must differ")
	}
}

func TestParsePHC(t *testing.T) {
	hash, _ := Hash("abcdefgh")
	params, salt, key, err := parsePHC(hash)
	if err != nil {
		t.Fatal(err)
	}
	if params.time != 3 || params.memory != 64*1024 || len(salt) != 16 || len(key) != 32 {
		t.Fatalf("bad parsed params: %+v", params)
	}
}

func TestInvalidHash(t *testing.T) {
	if Verify("x", "not-a-hash") {
		t.Fatal("invalid hash must fail verify")
	}
}
