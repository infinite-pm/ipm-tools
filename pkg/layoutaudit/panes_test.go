package layoutaudit

import (
	"strings"
	"testing"
)

// The rule the interaction exists to keep: once a reader has clicked, the
// picture stops moving — on a timer AND on hover. A diagram that changes
// while it is being studied is worse than one that never moved.
func TestPinnedModesAreNeverAnimatedOrHovered(t *testing.T) {
	// Every rule that applies to a PINNED row must switch the animation off;
	// one that forgets leaves the picture moving under a reader who asked it
	// to stop.
	for _, mode := range []string{ModeBefore, ModeFirst, ModeSecond} {
		prefix := ".row." + mode + " "
		found := 0
		for _, line := range strings.Split(PaneCSS, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, prefix) || !strings.Contains(line, "{") {
				continue
			}
			found++
			if !strings.Contains(line, "animation:none") {
				t.Errorf("a pinned %q rule leaves the animation running: %s", mode, line)
			}
		}
		if found == 0 {
			t.Errorf("no CSS rule for the pinned %q state", mode)
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
