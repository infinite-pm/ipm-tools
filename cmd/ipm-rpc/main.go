// Command ipm-rpc is the infinite.pm language server. It speaks the
// Language Server Protocol over stdio and serves IDE features for ipmt
// sources and for ipmt fences embedded in Markdown.
//
// This file's responsibility:
//   - flag parsing (--verbose, --quiet, --stdio, --version)
//   - structured logging via pkg/ipm/log
//   - stdio transport setup using jrpc2's LSP framing channel
//   - server lifecycle (run, signal handling, graceful shutdown)
//
// The actual LSP method dispatch lives in server.go. The workspace
// commands (ipm.embed / ipm.embedBuffer / ipm.check) are in commands.go
// and embed_buffer.go. Semantic-token emission is in tokens.go.
//
// Design notes: docs/ipm-rpc.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"

	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
)

const toolName = "ipm-rpc"

func main() {
	var (
		verbose     bool
		quiet       bool
		stdioFlag   bool
		versionFlag bool
	)
	flag.BoolVar(&verbose, "verbose", false, "log at DEBUG (default WARN)")
	flag.BoolVar(&quiet, "quiet", false, "log at ERROR only")
	// LSP clients (notably vscode-languageclient when `transport:
	// TransportKind.stdio` is set) unconditionally append `--stdio` to the
	// command line. stdio is our only transport, so the flag is a no-op
	// here — we just need to accept it so flag.Parse() doesn't error out
	// and exit before the LSP handshake. Same convention as gopls,
	// rust-analyzer, typescript-language-server.
	flag.BoolVar(&stdioFlag, "stdio", false, "speak LSP over stdio (always on; accepted for compatibility with LSP clients)")
	flag.BoolVar(&versionFlag, "version", false, "print version and exit (used by the VS Code extension to display server info)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [--verbose | --quiet] [--stdio] [--version]\n", toolName)
		fmt.Fprintln(os.Stderr, "\nSpeaks LSP over stdio. Spawned by an editor (VS Code, Neovim, etc.).")
		flag.PrintDefaults()
	}
	flag.Parse()
	_ = stdioFlag // silence linter; the flag itself does nothing.

	if versionFlag {
		fmt.Fprintf(os.Stdout, "%s %s\n", toolName, versionString())
		return
	}

	// Logs go to stderr; LSP traffic goes on stdin/stdout, so this is
	// the right channel for diagnostics that an editor can capture.
	iplog.SetupCLI(verbose, quiet)
	logger := iplog.Default().With(slog.String("component", toolName), slog.String("version", versionString()))
	logger.Info("starting up")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := newServer(logger)

	// LSP framing: Content-Length headers over stdin/stdout.
	ch := channel.Header("application/vscode-jsonrpc; charset=utf-8")(os.Stdin, os.Stdout)
	jrpcSrv := jrpc2.NewServer(srv.assignedHandlers(), &jrpc2.ServerOptions{
		Logger: jrpcLogger(logger),
		// LSP requires server→client notifications (publishDiagnostics,
		// window/logMessage, etc.). jrpc2 treats those as a non-standard
		// push extension; opt in explicitly.
		AllowPush: true,
	})
	jrpcSrv.Start(ch)

	// Hand the server a back-channel to publish notifications (publishDiagnostics)
	// against the live jrpc2 server.
	srv.attachJRPC(jrpcSrv)

	// Wait for the server to be done OR the process to receive a signal.
	done := make(chan error, 1)
	go func() { done <- jrpcSrv.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			logger.Warn("server exited with error", slog.String("err", err.Error()))
		}
	case <-ctx.Done():
		logger.Info("signal received; shutting down")
		jrpcSrv.Stop()
		<-done
	}
	logger.Info("goodbye")

	// Apply the LSP exit code selected by the `exit` handler: 0 when a
	// `shutdown` request preceded `exit`, 1 otherwise. When the server wound
	// down for another reason (transport close, signal), exitStatus() is 0.
	if code := srv.exitStatus(); code != 0 {
		os.Exit(code)
	}
}

// jrpcLogger adapts a slog.Logger to jrpc2.Logger, which is a
// `func(string)` typed receiver — jrpc2 emits pre-formatted strings,
// no fmt-style args.
func jrpcLogger(l *slog.Logger) jrpc2.Logger {
	return func(text string) { l.Debug(text) }
}

func versionString() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}
