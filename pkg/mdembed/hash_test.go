package mdembed

import "testing"

func TestNormalizeIPMT_idempotent(t *testing.T) {
	src := "A --> B\n  \nB --> C\n\n\n"
	want := "A --> B\n\nB --> C"
	got := NormalizeIPMT(src)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if NormalizeIPMT(got) != got {
		t.Fatalf("not idempotent")
	}
}

func TestHashIPMT_stableAcrossCosmetics(t *testing.T) {
	a := "A --> B\nB --> C"
	b := "A --> B\nB --> C\n"         // trailing newline
	c := "A --> B   \nB --> C"        // trailing whitespace on a line
	d := "A --> B\r\nB --> C\r\n\r\n" // CRLF + extra blank
	hashes := []string{HashIPMT(a), HashIPMT(b), HashIPMT(c), HashIPMT(d)}
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Fatalf("hash[%d]=%s differs from hash[0]=%s; sources should normalize equal", i, hashes[i], hashes[0])
		}
	}
	if len(hashes[0]) != HashPrefixLen {
		t.Fatalf("hash length = %d, want %d", len(hashes[0]), HashPrefixLen)
	}
}

func TestHashIPMT_differentForDifferentSources(t *testing.T) {
	a := HashIPMT("A --> B")
	b := HashIPMT("A --> C")
	if a == b {
		t.Fatalf("expected different hashes, got %s == %s", a, b)
	}
}
