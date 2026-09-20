package model

import "strings"

// A KIND TAG is the workaround for one word being two kinds. ipmt identity is
// the canonical name alone, so `controller ::t` and `controller ::c` in one
// fence are one node; until the language says otherwise (identity by kind
// and name is an open proposal), an author writes the second as
// `controller (c) ::c` — the same word, tagged with the kind it is here. The
// tag is for the fence: everything downstream shows the word without it
// (layout labels, the still picture, the canvas), and a union keys the node
// by kind and the untagged name, so the tagged concept in a hand-written
// page and the concept a tool extracted from the same docs are one node.
//
// The three tags are " (t)", " (e)", " (c)": a space, the kind letter in
// parentheses, at the end of the name.

// KindTag returns the kind a name is tagged with ("t", "e" or "c") and the
// name without the tag; "" and the name unchanged when there is no tag.
func KindTag(name string) (tag, bare string) {
	t := strings.TrimSpace(name)
	if len(t) >= 4 && t[len(t)-1] == ')' && t[len(t)-3] == '(' && t[len(t)-4] == ' ' {
		switch k := t[len(t)-2]; k {
		case 't', 'e', 'c':
			return string(k), strings.TrimSpace(t[:len(t)-4])
		}
	}
	return "", name
}

// DisplayName is the name a reader sees: the kind tag, if any, dropped.
func DisplayName(name string) string {
	_, bare := KindTag(name)
	return bare
}
