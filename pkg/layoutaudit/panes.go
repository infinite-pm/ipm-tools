package layoutaudit

import "html/template"

// The before/after pane pair, shared by every report this package backs.
//
// The right pane holds ONE diagram in two states — the render as it is
// ("first"), and the same render with the differences drawn over it
// ("second") — plus "auto", which alternates them. Alternation is what makes
// a change findable in a second; two static pictures side by side leave the
// reader hunting.
//
// The interaction rule that matters: a CLICK PINS. Once the reader has
// clicked, the picture never changes again on its own — not on a timer and
// not on hover — because a diagram that moves while it is being studied is
// worse than one that never moved at all. Further clicks toggle first ⇄
// second; only the "auto" control brings the alternation back.
//
// CSS, controls and script live here as three constants rather than in each
// report's template, so two reports cannot drift into behaving differently
// while looking identical.

// PaneModes are the three states, in the order the controls present them.
const (
	ModeFirst  = "first"  // the diagram as rendered
	ModeSecond = "second" // the same diagram with the differences marked
	ModeAuto   = "auto"   // alternate between them
)

// PaneCSS styles the panes and implements the three modes.
const PaneCSS = `
.panes{display:grid;grid-template-columns:1fr 1fr;gap:1px;background:var(--line)}
.pane{background:var(--pane);padding:10px;position:relative}
.pane h4{margin:0 0 6px;font-size:11px;letter-spacing:.6px;text-transform:uppercase;color:#5c636b;
  display:flex;justify-content:space-between;align-items:center;gap:8px;min-height:22px}
.pane svg{display:block;height:auto;max-width:100%}
.pane-new .svgwrap{cursor:pointer}
.modes{display:inline-flex;gap:3px;text-transform:none;letter-spacing:0}
.modes button{font:inherit;font-size:11px;line-height:1.6;padding:0 7px;border-radius:5px;
  border:1px solid #d3d7dd;background:#fff;color:#3b4148;cursor:pointer}
.modes button:hover{border-color:#9aa3ad}
.modes button[aria-pressed="true"]{background:#3b4148;border-color:#3b4148;color:#fff;font-weight:600}
.modes .glyph{margin-right:3px}

/* The overlay layer: hidden in "first", shown in "second", alternating in
   "auto". Nothing hovers it into view once a mode is pinned. */
.audit-overlay{opacity:0}
.row.auto .pane-new .audit-overlay{animation:flap 2.4s ease-in-out infinite}
.row.auto .pane-new .svgwrap:hover .audit-overlay{opacity:1;animation:none}
.row.first .pane-new .audit-overlay{opacity:0;animation:none}
.row.second .pane-new .audit-overlay{opacity:1;animation:none}
@keyframes flap{0%,42%{opacity:0}52%,92%{opacity:1}100%{opacity:0}}
@media (prefers-reduced-motion: reduce){
  /* No timer at all: the reader picks a state, or hovers to peek. */
  .row.auto .pane-new .audit-overlay{animation:none;opacity:0}
}
`

// PaneControls is the three-way control that sits above the right diagram.
// Clicking a control does exactly what clicking the image does, which is why
// both are wired to one function.
const PaneControls = `<span class="modes">
        <button type="button" data-mode="first" title="the diagram as rendered"><span class="glyph">▢</span>first</button>
        <button type="button" data-mode="second" title="the same diagram with the differences marked"><span class="glyph">◆</span>second</button>
        <button type="button" data-mode="auto" title="alternate between the two"><span class="glyph">⟳</span>auto</button>
      </span>`

// PaneJS wires the controls, the image click and the keyboard shortcuts.
const PaneJS = `
const MODES = ['first','second','auto'];

function setMode(row, mode){
  MODES.forEach(m => row.classList.toggle(m, m === mode));
  row.querySelectorAll('.modes button').forEach(b =>
    b.setAttribute('aria-pressed', String(b.dataset.mode === mode)));
}
function modeOf(row){ return MODES.find(m => row.classList.contains(m)) || 'auto'; }

// Clicking the image pins it. From "auto" that means stopping on the marked
// state — holding the highlight is why anyone clicks — and from then on a
// click just toggles the two. Getting back to alternating is a deliberate
// act: the "auto" control.
function pokeImage(row){
  setMode(row, modeOf(row) === 'second' ? 'first' : 'second');
}
function setAll(mode){ document.querySelectorAll('.row').forEach(r => setMode(r, mode)); }

document.addEventListener('click', e => {
  const btn = e.target.closest('.modes button');
  if (btn){ setMode(btn.closest('.row'), btn.dataset.mode); return; }
  const img = e.target.closest('.pane-new .svgwrap');
  if (img){ pokeImage(img.closest('.row')); }
});
document.addEventListener('keydown', e => {
  if (e.target.tagName === 'INPUT' || e.metaKey || e.ctrlKey) return;
  // Space is the panic key: stop every pane moving, showing what changed.
  if (e.key === ' '){ e.preventDefault(); setAll('second'); return; }
  const pick = {1:'first', 2:'second', a:'auto', f:'first', s:'second'}[e.key];
  if (pick) setAll(pick);
});
document.querySelectorAll('.row').forEach(r => setMode(r, modeOf(r)));
`

// PaneFuncs are the template helpers that inject the three constants. Reports
// call {{paneCSS}}, {{paneControls}} and {{paneJS}}.
func PaneFuncs() template.FuncMap {
	return template.FuncMap{
		"paneCSS":      func() template.CSS { return template.CSS(PaneCSS) },
		"paneControls": func() template.HTML { return template.HTML(PaneControls) }, //nolint:gosec // a constant
		"paneJS":       func() template.JS { return template.JS(PaneJS) },
	}
}
