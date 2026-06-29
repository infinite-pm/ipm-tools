//go:build l7notrace

package layout7

// The l7notrace build strips every trace emit site: traceEnabled is a
// compile-time false, so `if g.tracing()` branches (and their payload
// construction) are dead code the compiler removes.
const traceEnabled = false
