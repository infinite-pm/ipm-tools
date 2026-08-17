package layoutaudit

import (
	"strings"
	"testing"
)

// The rule the interaction exists to keep: once a reader has clicked, the
// picture stops moving — on a timer AND on hover. A diagram that changes
// while it is being studied is worse than one that never moved.
func TestOnlyAutoRowsCanAnimate(t *testing.T) {
	// Animation is opt-IN now: it is applied only under .row.auto.live, so a
	// pinned row cannot match an animating rule at all. That is a stronger
	// property than the old one (every pinned rule must say animation:none),
	// and it is the one worth enforcing — a single forgotten "animation:none"
	// used to leave a picture moving under a reader who asked it to stop.
	for _, line := range strings.Split(PaneCSS, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "animation:") || strings.Contains(line, "animation:none") {
			continue
		}
		// ".auto" as a token: the row may carry other classes first
		// (".row.no-before.auto.live"), which a prefix check would miss.
		if !strings.Contains(line, ".auto") {
			t.Errorf("an animation can reach a row that is not on auto: %s", line)
		}
	}
	// And hover may only reveal while alternating.
	for _, line := range strings.Split(PaneCSS, "\n") {
		if strings.Contains(line, ":hover") && strings.Contains(line, ".layer") {
			if !strings.Contains(line, ".auto") {
				t.Errorf("hover changes a pinned pane: %s", line)
			}
		}
	}
}

// Four controls, in the order a reader meets them, each naming a state.
func TestControlsOfferTheThreeStates(t *testing.T) {
	for _, mode := range []string{ModeBefore, ModeFirst, ModeSecond, ModeAuto} {
		if !strings.Contains(PaneControls, `data-mode="`+mode+`"`) {
			t.Errorf("no control for %q", mode)
		}
	}
	if strings.Index(PaneControls, ModeFirst) > strings.Index(PaneControls, ModeSecond) {
		t.Error(`"first" must be offered before "second"`)
	}
	if n := strings.Count(PaneControls, "<button"); n != 4 {
		t.Errorf("%d controls, want exactly 4", n)
	}
	if strings.Index(PaneControls, ModeBefore) > strings.Index(PaneControls, ModeFirst) {
		t.Error(`"before" must be offered first: it is what the new diagram is compared against`)
	}
	if !strings.Contains(PaneControls, "glyph") {
		t.Error("the controls carry no icon")
	}
}

// Clicking the image steps the pinned cycle and can never return to auto:
// getting the alternation back is a deliberate act on the control. Reading
// the current mode is fine; SETTING auto is what must not happen.
func TestImageClickStepsTheCycleAndNeverRestoresAuto(t *testing.T) {
	i := strings.Index(PaneJS, "function pokeImage")
	if i < 0 {
		t.Fatal("no image-click handler")
	}
	body := PaneJS[i : i+strings.Index(PaneJS[i:], "\n}")]
	if strings.Contains(body, "setMode(row, 'auto')") {
		t.Errorf("clicking the image can restore auto:\n%s", body)
	}
	if !strings.Contains(body, "CYCLE") {
		t.Errorf("the image click does not step the pinned cycle:\n%s", body)
	}
	// A row with no old diagram must skip "before" rather than showing blank.
	if !strings.Contains(body, "no-before") {
		t.Errorf("the click does not account for a missing before state:\n%s", body)
	}
	// Both the control and the image go through one function, or the two
	// entry points would drift.
	if strings.Count(PaneJS, "function setMode") != 1 {
		t.Error("mode setting is not in one place")
	}
	if !strings.Contains(PaneJS, ".modes button") || !strings.Contains(PaneJS, ".pane-new .stack") {
		t.Error("the controls and the image are not both wired")
	}
}

// A long history's report holds hundreds of diagrams. Inlining them cost
// 7.4 MB and 776 SVGs of DOM on one page, and animating them all at once took
// a VS Code webview down. Panes are lazy images and only on-screen rows move.
func TestOnlyVisibleRowsAnimate(t *testing.T) {
	for _, line := range strings.Split(PaneCSS, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "animation:cyc") && !strings.Contains(line, "animation:flap") {
			continue
		}
		if !strings.Contains(line, ".live") {
			t.Errorf("an animation runs regardless of visibility: %s", line)
		}
	}
	if !strings.Contains(PaneCSS, ".pane img") {
		t.Error("panes are not images; a long page would inline every diagram again")
	}
	if !strings.Contains(PaneJS, "IntersectionObserver") {
		t.Error("nothing marks rows as visible, so .live would never be set")
	}
	// And a row that has never been seen must still show something.
	if !strings.Contains(PaneCSS, ".row.auto:not(.live) .pane-new .stack > .layer-after") {
		t.Error("an unobserved row has no defined appearance")
	}
}

// A report opens STILL. Motion is opt-in: a page that starts animating
// decides for the reader what to look at, and these pages are read before
// they are compared.
func TestDefaultModeIsStill(t *testing.T) {
	if !strings.Contains(PaneJS, `|| 'first'`) {
		t.Error("a row with no mode class falls back to something other than first")
	}
	if strings.Contains(PaneJS, `|| 'auto'`) {
		t.Error("auto is still a fallback somewhere")
	}
}

// Specificity decides whether a picture exists. The reference pane was blank
// while holding a perfectly good diagram, because its base hide rule carried
// three classes and its show rule two. And the two panes share layer names,
// so an unscoped rule written for one governs the other as well.
func TestLayerRulesAreScopedAndCanWin(t *testing.T) {
	classes := func(sel string) int { return strings.Count(sel, ".") }
	type rule struct{ sel, body string }
	var rules []rule
	for _, line := range strings.Split(PaneCSS, "\n") {
		line = strings.TrimSpace(line)
		i := strings.Index(line, "{")
		if i < 0 || !strings.HasSuffix(line, "}") || strings.HasPrefix(line, "@") {
			continue
		}
		rules = append(rules, rule{sel: strings.TrimSpace(line[:i]), body: line[i+1 : len(line)-1]})
	}

	// Every rule that touches a layer must name its pane, or it reaches into
	// the other one — both panes have a "before".
	for _, r := range rules {
		if !strings.Contains(r.sel, ".layer-") {
			continue
		}
		if !strings.Contains(r.sel, ".pane-old") && !strings.Contains(r.sel, ".pane-new") {
			t.Errorf("layer rule is not scoped to a pane, so it governs both: %s", r.sel)
		}
	}

	// Within a pane, a rule that SHOWS a layer must be able to beat the base
	// rule that hides every layer of that pane.
	for _, pane := range []string{".pane-old", ".pane-new"} {
		base := 0
		for _, r := range rules {
			if strings.Contains(r.sel, pane) && strings.HasSuffix(r.sel, "> .layer") &&
				strings.Contains(r.body, "opacity:0") {
				base = classes(r.sel)
			}
		}
		if base == 0 {
			continue
		}
		for _, r := range rules {
			if !strings.Contains(r.sel, pane) || !strings.Contains(r.body, "opacity:1") {
				continue
			}
			if classes(r.sel) < base {
				t.Errorf("%q (%d classes) cannot beat the base hide (%d classes) — the pane renders blank",
					r.sel, classes(r.sel), base)
			}
		}
	}
}

// The controls must show which state is current, or a pinned pane looks the
// same as an alternating one that happens to be mid-cycle.
func TestActiveControlIsMarked(t *testing.T) {
	if !strings.Contains(PaneJS, "aria-pressed") {
		t.Error("the active control is not marked")
	}
	if !strings.Contains(PaneCSS, `aria-pressed="true"`) {
		t.Error("the active control has no styling")
	}
}
