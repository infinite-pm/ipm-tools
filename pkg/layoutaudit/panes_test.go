package layoutaudit

import (
	"strings"
	"testing"
)

// The rule the interaction exists to keep: once a reader has clicked, the
// picture stops moving — on a timer AND on hover. A diagram that changes
// while it is being studied is worse than one that never moved.
func TestPinnedModesAreNeverAnimatedOrHovered(t *testing.T) {
	for _, mode := range []string{ModeFirst, ModeSecond} {
		rule := ".row." + mode + " .pane-new .audit-overlay"
		i := strings.Index(PaneCSS, rule)
		if i < 0 {
			t.Fatalf("no CSS rule for the pinned %q state", mode)
		}
		body := PaneCSS[i : i+strings.Index(PaneCSS[i:], "}")]
		if !strings.Contains(body, "animation:none") {
			t.Errorf("%s does not stop the animation: %s", mode, body)
		}
	}
	// Hover may only reveal while alternating; a pinned pane ignores it.
	for _, line := range strings.Split(PaneCSS, "\n") {
		if strings.Contains(line, ":hover") && strings.Contains(line, "audit-overlay") {
			if !strings.HasPrefix(strings.TrimSpace(line), ".row.auto") {
				t.Errorf("hover changes a pinned pane: %s", line)
			}
		}
	}
}

// Three controls, in the order a reader meets them, each naming a state.
func TestControlsOfferTheThreeStates(t *testing.T) {
	for _, mode := range []string{ModeFirst, ModeSecond, ModeAuto} {
		if !strings.Contains(PaneControls, `data-mode="`+mode+`"`) {
			t.Errorf("no control for %q", mode)
		}
	}
	if strings.Index(PaneControls, ModeFirst) > strings.Index(PaneControls, ModeSecond) {
		t.Error(`"first" must be offered before "second"`)
	}
	if n := strings.Count(PaneControls, "<button"); n != 3 {
		t.Errorf("%d controls, want exactly 3", n)
	}
	if !strings.Contains(PaneControls, "glyph") {
		t.Error("the controls carry no icon")
	}
}

// Clicking the image toggles the two pinned states and can never return to
// auto: getting the alternation back is a deliberate act on the control.
func TestImageClickTogglesAndNeverRestoresAuto(t *testing.T) {
	i := strings.Index(PaneJS, "function pokeImage")
	if i < 0 {
		t.Fatal("no image-click handler")
	}
	body := PaneJS[i : i+strings.Index(PaneJS[i:], "\n}")]
	if strings.Contains(body, "'auto'") {
		t.Errorf("clicking the image can restore auto:\n%s", body)
	}
	if !strings.Contains(body, ModeFirst) || !strings.Contains(body, ModeSecond) {
		t.Errorf("the image click does not toggle first/second:\n%s", body)
	}
	// Both the control and the image go through one function, or the two
	// entry points would drift.
	if strings.Count(PaneJS, "function setMode") != 1 {
		t.Error("mode setting is not in one place")
	}
	if !strings.Contains(PaneJS, ".modes button") || !strings.Contains(PaneJS, ".pane-new .svgwrap") {
		t.Error("the controls and the image are not both wired")
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
