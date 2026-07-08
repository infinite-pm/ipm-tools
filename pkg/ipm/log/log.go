// Package log provides a thin convention layer on top of log/slog for the
// infinite.pm Go toolchain (cmd/md-embed, cmd/ipm-rpc, and the shared
// pkg/ipm/* packages).
//
// Why a wrapper at all when log/slog already exists?
//
//   - We want one place that knows how to switch between a text handler
//     (for CLI invocations and developer terminals) and a JSON handler
//     (for the LSP server, where the host editor reads structured logs).
//   - Shared packages should not import VS Code- or LSP-specific code;
//     the wrapper lets cmd/ipm-rpc inject a handler that publishes log
//     records as `window/logMessage` notifications without leaking that
//     dependency into pkg/ipm/*.
//   - Tests need a no-op default that doesn't pollute test output.
//
// Default (when no init function is called): records at WARN+ go to stderr
// in text form; INFO/DEBUG are dropped. CLIs override with SetupCLI() to get
// human-readable output; ipm-rpc will install its own handler that forwards
// records to the LSP client.
//
// Per an earlier design note.
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// loggerKey is the unexported context key for attaching a per-request logger
// to a context (e.g. inside an LSP method handler, before dispatching to
// pkg/ipm/parser or pkg/ipm/validate). Callers fetch via FromContext.
type loggerKey struct{}

// rootLogger is the package-level fallback used when no context-scoped
// logger is in play. Defaults to slog.Default() shaped to WARN+ on stderr;
// SetupCLI / SetupRPC replace it.
var rootLogger *slog.Logger = newDefault()

func newDefault() *slog.Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	return slog.New(h)
}

// SetupCLI configures the package-level logger for command-line use:
// human-readable text on stderr, level controlled by verbose. Use this from
// cmd/md-embed and any future CLI's main() before parsing arguments.
func SetupCLI(verbose, quiet bool) {
	level := slog.LevelWarn
	switch {
	case quiet:
		level = slog.LevelError
	case verbose:
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	rootLogger = slog.New(h)
}

// SetupRPC configures the package-level logger to forward records via the
// given handler. cmd/ipm-rpc installs an adapter that wraps each record into
// a `window/logMessage` LSP notification; pkg/ipm/* callers never see the
// LSP dependency.
//
// Pass nil to fall back to the default (WARN+ on stderr).
func SetupRPC(h slog.Handler) {
	if h == nil {
		rootLogger = newDefault()
		return
	}
	rootLogger = slog.New(h)
}

// SetupDiscard silences logging entirely. For use in tests.
func SetupDiscard() {
	rootLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Default returns the package-level logger. Use this from any function that
// doesn't carry a context.Context.
func Default() *slog.Logger { return rootLogger }

// FromContext returns the context-scoped logger if one is attached, else
// Default(). Use this from LSP handlers that thread context down into
// pkg/ipm/* — each request can have its own attributes (method, id, file)
// attached up front so every log record from the request inherits them.
func FromContext(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && v != nil {
		return v
	}
	return rootLogger
}

// With returns a context with the given logger attached. The logger is what
// FromContext will return for any descendant context.
func With(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}
