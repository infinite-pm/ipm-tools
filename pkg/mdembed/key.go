package mdembed

// Embed ids can be 3-char base-36 keys ("000".."zzz") ordered lexicographically,
// so a NEW block is given a key BETWEEN its document neighbours (the midpoint of
// the interval) — inserting or reordering blocks never forces a renumber, and a
// duplicated key (copy-paste) is reassigned a fresh between-key instead of being
// silently skipped. Fresh docs start at "100", leaving "000".."0zz" of head-room
// for prepends; the default step ("100"->"110") leaves a full last-char between
// neighbours for later inserts.
//
// Every doc uses this scheme: an existing marker id is always preserved (a key,
// an include's filename, or a hand-written id=), and only blocks that need a
// fresh id — new blocks and duplicates — are assigned a between-key.

const (
	keyAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz" // base 36, ascending
	keyWidth    = 3
	keyMaxInt   = 36*36*36 - 1 // "zzz" = 46655
	keyFirstInt = 36 * 36      // "100" = 1296
	keyStepInt  = 36           // "100" -> "110": a full last char of insert room
)

func keyDigit(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'z':
		return int(b-'a') + 10
	}
	return -1
}

// keyToInt parses a 3-char base-36 key; ok=false for anything else.
func keyToInt(k string) (int, bool) {
	if len(k) != keyWidth {
		return 0, false
	}
	n := 0
	for i := range keyWidth {
		d := keyDigit(k[i])
		if d < 0 {
			return 0, false
		}
		n = n*36 + d
	}
	return n, true
}

// intToKey renders n (clamped to [0,46655]) as a 3-char base-36 key.
func intToKey(n int) string {
	if n < 0 {
		n = 0
	}
	if n > keyMaxInt {
		n = keyMaxInt
	}
	return string([]byte{keyAlphabet[n/1296], keyAlphabet[(n/36)%36], keyAlphabet[n%36]})
}

// IsKey reports whether s is a valid 3-char base-36 key.
func IsKey(s string) bool { _, ok := keyToInt(s); return ok }

// keepID reports whether a block keeps the id resolved for it (vs. being
// reassigned a fresh between-key). Any id it already has is intentional — an
// existing marker's id (a base-36 key or a hand-written id=), or an include's
// filename, which carries meaning (swap.ipm.svg) — so it is preserved and its
// SVG never renamed. Only a block with no id at all (a new visible block) is
// keyed here; the caller additionally gates on first-seen, so a duplicated id
// is re-keyed rather than colliding.
func keepID(br BlockResult) bool {
	return br.NewMarker.ID != ""
}

// keyBetween returns a key strictly between prev and next, ok=false when the
// interval has no room for another 3-char key. An empty prev means the bottom of
// the keyspace, an empty next the top. With both empty (a fresh doc) it returns
// keyFirst ("100"). A non-base-36 bound is treated as the matching open end.
func keyBetween(prev, next string) (string, bool) {
	lo, hi := -1, keyMaxInt+1
	if v, ok := keyToInt(prev); ok {
		lo = v
	}
	if v, ok := keyToInt(next); ok {
		hi = v
	}
	if prev == "" && next == "" {
		return intToKey(keyFirstInt), true // fresh doc starts at "100"
	}
	mid := (lo + hi) / 2
	if mid <= lo || mid >= hi {
		return "", false
	}
	return intToKey(mid), true
}

// keyAfter returns the next key a step beyond prev (leaving a last-char of room),
// or the midpoint to the ceiling near the top. Empty prev yields keyFirst.
func keyAfter(prev string) (string, bool) {
	v, ok := keyToInt(prev)
	if !ok {
		return intToKey(keyFirstInt), true // "100"
	}
	if v+keyStepInt <= keyMaxInt {
		return intToKey(v + keyStepInt), true
	}
	return keyBetween(prev, "")
}
