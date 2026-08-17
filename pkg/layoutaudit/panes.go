package layoutaudit

import "html/template"

// The before/after pane pair, shared by every report this package backs.
//
// The right pane holds THREE states in one place: the old diagram
// ("before"), the new one ("first"), and the new one with the differences
// drawn over it ("second"). "auto" runs them in that order.
//
// A row opens on "first" and stands still. Alternation is a tool the reader
// reaches for, not a state the page starts in: a report is opened to be read
// first and compared second, and a page that moves on arrival decides for the
// reader where to look.
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

// Each state is a SEPARATE IMAGE, not a layer inside one SVG.
//
// Inlining cost 7.4 MB and 776 diagrams of DOM on one page. Behind
// <img loading="lazy"> the page is a few hundred bytes per row and the
// browser decodes only what is on screen — but CSS cannot reach inside an
// <img> to toggle a group, so "marked" is its own file rather than an
// overlay switched on within the picture.

// PaneCycle is the pinned order a click steps through.
var PaneCycle = []string{ModeBefore, ModeFirst, ModeSecond}

// The LEFT pane's two states. It is the reference the right pane is read
// against, and which reference is wanted depends on the question: the column
// before this one ("what did this change do"), or what ships today ("how far
// from current is this"). An old column is nearly always read with the second
// question in mind, so it must not require opening another report.
const (
	LeftBefore  = "before"  // the column immediately before this one
	LeftCurrent = "current" // the newest engine's rendering of the same diagram
)

// PaneCSS styles the panes and implements the three modes.
const PaneCSS = `
.panes{display:grid;grid-template-columns:1fr 1fr;gap:1px;background:var(--line)}
.pane{background:var(--pane);padding:10px;position:relative}
.pane h4{margin:0 0 6px;font-size:11px;letter-spacing:.6px;text-transform:uppercase;color:#5c636b;
  display:flex;justify-content:space-between;align-items:center;gap:8px;min-height:22px}
.pane img{display:block;width:100%;height:auto}
.pane-new .stack{cursor:pointer}
.modes,.lefts{display:inline-flex;gap:3px;text-transform:none;letter-spacing:0}
.modes button,.lefts button{font:inherit;font-size:11px;line-height:1.6;padding:0 7px;border-radius:5px;
  border:1px solid #d3d7dd;background:#fff;color:#3b4148;cursor:pointer}
.modes button:hover,.lefts button:hover{border-color:#9aa3ad}
.modes button[aria-pressed="true"],.lefts button[aria-pressed="true"]{
  background:#3b4148;border-color:#3b4148;color:#fff;font-weight:600}
.modes .glyph,.lefts .glyph{margin-right:3px}

/* The images of one pane occupy ONE grid cell, so they share an origin: same
   top-left, same scale (the widths come from PaneWidths). That registration
   is what makes the swap readable — anything that holds still did not move. */
.stack{display:grid}
.stack > .layer{grid-area:1/1;position:relative;align-self:start;justify-self:start}
.layer .chip{position:absolute;top:0;right:0;font-size:10px;line-height:1.5;padding:0 6px;
  border-radius:0 0 0 5px;background:#3b4148;color:#fff;letter-spacing:.4px}

/* EVERY layer rule names the pane it belongs to. The panes share layer names —
   "before" means the same picture in each — so an unscoped rule written for
   one silently governs the other. And all rules of a pane share a selector
   shape: a base hide of three classes once beat a show of two, and the pane
   rendered blank while holding a perfectly good diagram. */

/* LEFT — the reference: the column before this one, or what ships today. */
.pane-old .stack > .layer{opacity:0}
.pane-old .stack > .layer-before{opacity:1}
.row.leftcurrent .pane-old .stack > .layer-before{opacity:0}
.row.leftcurrent .pane-old .stack > .layer-current{opacity:1}

/* RIGHT — three states, one visible at a time. Nothing animates and nothing
   responds to hover once a reader has chosen. */
.pane-new .stack > .layer{opacity:0}
.row.before .pane-new .stack > .layer-before{opacity:1}
.row.first  .pane-new .stack > .layer-after{opacity:1}
.row.second .pane-new .stack > .layer-marked{opacity:1}

/* auto: before → first → second, on one timeline so the three images can
   never disagree about which state is showing, and ONLY WHILE ON SCREEN
   (.live, set by an IntersectionObserver) — a report of a long history
   carries hundreds of rows, and animating them all at once took a VS Code
   webview down. */
.row.auto.live .pane-new .stack > .layer-before{animation:cyc-a 3.6s linear infinite}
.row.auto.live .pane-new .stack > .layer-after{animation:cyc-b 3.6s linear infinite}
.row.auto.live .pane-new .stack > .layer-marked{animation:cyc-c 3.6s linear infinite}
@keyframes cyc-a{0%,30%{opacity:1}34%,100%{opacity:0}}
@keyframes cyc-b{0%,30%{opacity:0}34%,63%{opacity:1}67%,100%{opacity:0}}
@keyframes cyc-c{0%,63%{opacity:0}67%,96%{opacity:1}100%{opacity:0}}

/* A row with no "before" (the old engine could not lay it out) alternates the
   remaining two instead, and hides the control that would show nothing. */
.row.no-before.auto.live .pane-new .stack > .layer-after{animation:cyc-nb-a 2.4s linear infinite}
.row.no-before.auto.live .pane-new .stack > .layer-marked{animation:cyc-nb-b 2.4s linear infinite}
@keyframes cyc-nb-a{0%,45%{opacity:1}50%,100%{opacity:0}}
@keyframes cyc-nb-b{0%,45%{opacity:0}50%,95%{opacity:1}100%{opacity:0}}
.row.no-before .modes button[data-mode="before"]{display:none}

/* Until the observer speaks — no JS, or a row never scrolled to — the new
   diagram is what shows, and nothing moves. Safe by default. */
.row.auto:not(.live) .pane-new .stack > .layer-after{opacity:1}

@media (prefers-reduced-motion: reduce){
  .row.auto .pane-new .stack > .layer{animation:none;opacity:0}
  .row.auto .pane-new .stack > .layer-after{animation:none;opacity:1}
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

// LeftControls switches the reference pane between the column before this one
// and the current engine's rendering. Rendered only when a "current" exists.
// "before" is deliberately the same word the right pane uses for the same
// picture: one vocabulary, or the reader has to hold two.
const LeftControls = `<span class="lefts">
        <button type="button" data-left="before" title="the column before this one"><span class="glyph">◑</span>before</button>
        <button type="button" data-left="current" title="what the newest engine draws today"><span class="glyph">★</span>current</button>
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
// The default is "first": the new diagram, standing still. Motion is opt-in,
// because a page that starts moving decides for the reader what to look at,
// and a report is opened to be read before it is compared.
function modeOf(row){ return MODES.find(m => row.classList.contains(m)) || 'first'; }

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

// The left pane's reference: previous column, or what ships today.
function setLeft(row, which){
  row.classList.toggle('leftcurrent', which === 'current');
  row.querySelectorAll('.lefts button').forEach(b =>
    b.setAttribute('aria-pressed', String(b.dataset.left === which)));
}

document.addEventListener('click', e => {
  const lb = e.target.closest('.lefts button');
  if (lb){ setLeft(lb.closest('.row'), lb.dataset.left); return; }
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
document.querySelectorAll('.row').forEach(r => {
  setMode(r, modeOf(r));
  setLeft(r, r.classList.contains('leftcurrent') ? 'current' : 'before');
});

// Animate only what is on screen, and let the browser fetch only what is on
// screen: the images are lazy, so a page of two hundred rows costs the
// viewport, not the history.
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
		"leftControls": func() template.HTML { return template.HTML(LeftControls) }, //nolint:gosec // a constant
		"paneJS":       func() template.JS { return template.JS(PaneJS) },
	}
}
