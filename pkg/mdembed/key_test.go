package mdembed

import "testing"

func TestKeyRoundTrip(t *testing.T) {
	for _, k := range []string{"000", "100", "1z0", "200", "zzz", "abc"} {
		n, ok := keyToInt(k)
		if !ok {
			t.Fatalf("keyToInt(%q) not ok", k)
		}
		if got := intToKey(n); got != k {
			t.Errorf("roundtrip %q -> %d -> %q", k, n, got)
		}
	}
	if _, ok := keyToInt("12"); ok {
		t.Error("2-char id must be invalid")
	}
	if _, ok := keyToInt("12!"); ok {
		t.Error("bad char must be invalid")
	}
	if !IsKey("100") || IsKey("1") || IsKey("ABCD") {
		t.Error("IsKey width/charset wrong")
	}
}

func TestKeyBetween(t *testing.T) {
	mid, ok := keyBetween("100", "110")
	if !ok || mid <= "100" || mid >= "110" {
		t.Fatalf("between 100/110 = %q ok=%v, want strictly inside", mid, ok)
	}
	if k, ok := keyBetween("", ""); !ok || k != "100" {
		t.Fatalf("fresh doc start = %q ok=%v, want 100", k, ok)
	}
	if k, ok := keyBetween("", "100"); !ok || k >= "100" {
		t.Fatalf("prepend = %q ok=%v, want < 100", k, ok)
	}
	if _, ok := keyBetween("100", "101"); ok {
		t.Error("adjacent keys must have no midpoint")
	}
}

func TestKeyAfter(t *testing.T) {
	if k, ok := keyAfter("100"); !ok || k != "110" {
		t.Fatalf("after 100 = %q ok=%v, want 110", k, ok)
	}
	if k, ok := keyAfter(""); !ok || k != "100" {
		t.Fatalf("after empty = %q, want 100", k)
	}
}
