package layoutaudit

import "html/template"

// The before/after pane pair, shared by every report this package backs.
//
// The right pane cycles THREE states in one place: the old diagram
// ("before"), the new one ("first"), and the new one with the differences
// drawn over it ("second"). "auto" runs them in that order.
//
// Showing the old diagram in the RIGHT pane, at the same pixel scale and the
// same origin as the new one, is what makes this a blink comparator: a node
// that did not move stays perfectly still while the picture swaps, so the one
// that did move is the only thing that jumps. The left pane keeps both
// visible at once for reading; the right pane is for finding.
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

// The pane's states, in the order the controls present them and "auto"
// cycles them.
const (
	ModeBefore = "before" // the OLD diagram, in the right pane's own frame
	ModeFirst  = "first"  // the new diagram as rendered
	ModeSecond = "second" // the new diagram with the differences marked
	ModeAuto   = "auto"   // run before → first → second, repeatedly
)

// PaneCycle is the pinned order a click steps through.
var PaneCycle = []string{ModeBefore, ModeFirst, ModeSecond}

// PaneCSS styles the panes and implements the three modes.
const PaneCSS = `
.panes{display:grid;grid-template-columns:1fr 1fr;gap:1px;background:var(--line)}
.pane{background:var(--pane);padding:10px;position:relative}
.pane h4{margin:0 0 6px;font-size:11px;letter-spacing:.6px;text-transform:uppercase;color:#5c636b;
  display:flex;justify-content:space-between;align-items:center;gap:8px;min-height:22px}
.pane svg{display:block;height:auto;max-width:100%}
.pane-new .stack{cursor:pointer}
.modes{display:inline-flex;gap:3px;text-transform:none;letter-spacing:0}
.modes button{font:inherit;font-size:11px;line-height:1.6;padding:0 7px;border-radius:5px;
  border:1px solid #d3d7dd;background:#fff;color:#3b4148;cursor:pointer}
.modes button:hover{border-color:#9aa3ad}
.modes button[aria-pressed="true"]{background:#3b4148;border-color:#3b4148;color:#fff;font-weight:600}
.modes .glyph{margin-right:3px}

/* Both diagrams occupy ONE grid cell, so they share an origin: same top-left,
   same pixel scale (the widths come from PaneWidths). That registration is
   what makes the swap readable — anything that holds still did not move. */
.stack{display:grid}
.stack > .layer{grid-area:1/1;position:relative;align-self:start;justify-self:start}
.layer-before{opacity:0}
.layer .chip{position:absolute;top:0;right:0;font-size:10px;line-height:1.5;padding:0 6px;
  border-radius:0 0 0 5px;background:#3b4148;color:#fff;letter-spacing:.4px}

/* Pinned states. Nothing here animates, and nothing responds to hover: once a
   reader has chosen, the picture holds still until they choose again. */
.row.before .layer-before{opacity:1;animation:none}
.row.before .layer-after{opacity:0;animation:none}
.row.first  .layer-before{opacity:0;animation:none}
.row.first  .layer-after{opacity:1;animation:none}
.row.first  .audit-overlay{opacity:0;animation:none}
.row.second .layer-before{opacity:0;animation:none}
.row.second .layer-after{opacity:1;animation:none}
.row.second .audit-overlay{opacity:1;animation:none}

/* auto: before → first → second, on one timeline so the three layers can
   never disagree about which state is showing.
   ONLY WHILE ON SCREEN (.live, set by an IntersectionObserver). A report of a
   long history carries hundreds of diagrams; animating every one of them at
   once is not a nicety to fix later — 843 continuously animating SVG layers
   took a VS Code webview down. Off-screen rows hold still and cost nothing. */
.audit-overlay{opacity:0}
.row.auto.live .layer-before{animation:cyc-before 3.6s linear infinite}
.row.auto.live .layer-after{animation:cyc-after 3.6s linear infinite}
.row.auto.live .audit-overlay{animation:cyc-marks 3.6s linear infinite}
@keyframes cyc-before{0%,30%{opacity:1}34%,100%{opacity:0}}
@keyframes cyc-after{0%,30%{opacity:0}34%,100%{opacity:1}}
@keyframes cyc-marks{0%,63%{opacity:0}67%,96%{opacity:1}100%{opacity:0}}

/* A row whose old diagram could not be rendered (the engine failed on it)
   has no "before" to show, so auto falls back to the two-state alternation. */
.row.no-before.auto .layer-after{animation:none;opacity:1}
.row.no-before.auto.live .audit-overlay{animation:flap 2.4s ease-in-out infinite}
.row.no-before .modes button[data-mode="before"]{display:none}
@keyframes flap{0%,42%{opacity:0}52%,92%{opacity:1}100%{opacity:0}}

/* Until the observer speaks — no JS, or a row that has never been on screen —
   a row shows its "after" state and nothing moves. Safe by default. */
.row.auto:not(.live) .layer-before{opacity:0}
.row.auto:not(.live) .layer-after{opacity:1}

@media (prefers-reduced-motion: reduce){
  /* No timer at all: the reader picks a state with the controls. */
  .row.auto .layer-before{animation:none;opacity:0}
  .row.auto .layer-after{animation:none;opacity:1}
  .row.auto .audit-overlay{animation:none;opacity:0}
}
`

// PaneControls is the three-way control that sits above the right diagram.
// Clicking a control does exactly what clicking the image does, which is why
// both are wired to one function.
const PaneControls = `<span class="modes">
        <button type="button" data-mode="before" title="the OLD diagram, in this frame"><span class="glyph">◑</span>before</button>
        <button type="button" data-mode="first" title="the new diagram as rendered"><span class="glyph">▢</span>first</button>
        <button type="button" data-mode="second" title="the new diagram with the differences marked"><span class="glyph">◆</span>second</button>
        <button type="button" data-mode="auto" title="cycle before → first → second"><span class="glyph">⟳</span>auto</button>
      </span>`

// PaneJS wires the controls, the image click and the keyboard shortcuts.
const PaneJS = `
const CYCLE = ['before','first','second'];
const MODES = CYCLE.concat(['auto']);

function setMode(row, mode){
  if (mode === 'before' && row.classList.contains('no-before')) mode = 'first';
  MODES.forEach(m => row.classList.toggle(m, m === mode));
  row.querySelectorAll('.modes button').forEach(b =>
    b.setAttribute('aria-pressed', String(b.dataset.mode === mode)));
}
function modeOf(row){ return MODES.find(m => row.classList.contains(m)) || 'auto'; }

// Clicking the image pins it. From "auto" that means stopping on the marked
// state — holding the highlight is why anyone clicks — and from then on each
// click steps the cycle: second → before → first → second. Getting the
// alternation back is a deliberate act: the "auto" control.
function pokeImage(row){
  const cur = modeOf(row);
  if (cur === 'auto'){ setMode(row, 'second'); return; }
  const order = row.classList.contains('no-before') ? ['first','second'] : CYCLE;
  setMode(row, order[(order.indexOf(cur) + 1) % order.length]);
}
function setAll(mode){ document.querySelectorAll('.row').forEach(r => setMode(r, mode)); }

document.addEventListener('click', e => {
  const btn = e.target.closest('.modes button');
  if (btn){ setMode(btn.closest('.row'), btn.dataset.mode); return; }
  const img = e.target.closest('.pane-new .stack');
  if (img){ pokeImage(img.closest('.row')); }
});
document.addEventListener('keydown', e => {
  if (e.target.tagName === 'INPUT' || e.metaKey || e.ctrlKey) return;
  // Space is the panic key: stop every pane moving, showing what changed.
  if (e.key === ' '){ e.preventDefault(); setAll('second'); return; }
  const pick = {0:'before', 1:'first', 2:'second', a:'auto', b:'before'}[e.key];
  if (pick) setAll(pick);
});
document.querySelectorAll('.row').forEach(r => setMode(r, modeOf(r)));

// Animate only what is on screen. Without this every row in the report runs
// three CSS animations for as long as the page is open, which on a long
// history means hundreds of them at once — enough to take a webview down.
if (window.IntersectionObserver){
  const io = new IntersectionObserver(
    es => es.forEach(e => e.target.classList.toggle('live', e.isIntersecting)),
    {rootMargin: '150px'});
  document.querySelectorAll('.row').forEach(r => io.observe(r));
} else {
  document.querySelectorAll('.row').forEach(r => r.classList.add('live'));
}
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
