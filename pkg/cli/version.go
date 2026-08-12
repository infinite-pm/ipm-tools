package cli

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
)

// Version reports the module version this binary was built from: the tag, when
// the build came from a tagged commit; a pseudo-version from an untagged one;
// "(devel)" from a working tree, or from any build with VCS stamping off.
//
// It is read from the build info the toolchain embeds, so nothing has to be
// passed through -ldflags and no constant can drift from the tag it claims.
// The one thing it does need is a checkout that HAS the tag: a shallow clone
// fetched by commit rather than by tag leaves the binary reporting a
// pseudo-version, which is a build-pipeline bug rather than a code one.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// VersionFlag registers --version on fs and returns the check to run after
// Parse: it writes "<tool> <version>" to w and reports whether the command
// should stop rather than do its work.
//
// It is shared rather than written per command so that every binary in a
// release archive answers the question the same way. Before this, only ipm-rpc
// could be asked — it is the one the VS Code extension interrogates — and the
// other six exited with "flag provided but not defined: -version", which is a
// poor answer from a tool that was downloaded as part of a versioned tarball.
func VersionFlag(fs *flag.FlagSet, tool string) func(w io.Writer) bool {
	show := fs.Bool("version", false, "print version and exit")
	return func(w io.Writer) bool {
		if !*show {
			return false
		}
		fmt.Fprintf(w, "%s %s\n", tool, Version())
		return true
	}
}
