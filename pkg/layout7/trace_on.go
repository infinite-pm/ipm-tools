//go:build !l7notrace

package layout7

// traceEnabled is the compile-time debug switch (v7 debug architecture,
// docs/dev/layout-gen/layout-debug.md): the default build keeps the
// trace emit sites; `-tags l7notrace` compiles them away.
const traceEnabled = true
