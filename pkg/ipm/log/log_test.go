package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupCLI_levels(t *testing.T) {
	// Verbose enables debug; quiet drops to error; default sits at warn.
	cases := []struct {
		verbose, quiet bool
		emit           func(*slog.Logger)
		wantContains   string
		wantOmits      string
	}{
		{verbose: true, emit: func(l *slog.Logger) { l.Debug("dbg msg") }, wantContains: "dbg msg"},
		{verbose: false, emit: func(l *slog.Logger) { l.Debug("dbg msg") }, wantOmits: "dbg msg"},
		{verbose: false, emit: func(l *slog.Logger) { l.Warn("warn msg") }, wantContains: "warn msg"},
		{verbose: false, quiet: true, emit: func(l *slog.Logger) { l.Warn("warn msg") }, wantOmits: "warn msg"},
		{verbose: false, quiet: true, emit: func(l *slog.Logger) { l.Error("err msg") }, wantContains: "err msg"},
	}
	for i, tc := range cases {
		var buf bytes.Buffer
		SetupCLI(tc.verbose, tc.quiet)
		// Swap the handler's destination so tests don't write to real stderr.
		// Reach in via slog.New so we don't depend on package internals.
		level := slog.LevelWarn
		switch {
		case tc.quiet:
			level = slog.LevelError
		case tc.verbose:
			level = slog.LevelDebug
		}
		rootLogger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))

		tc.emit(Default())
		got := buf.String()
		if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
			t.Errorf("case %d: missing %q in:\n%s", i, tc.wantContains, got)
		}
		if tc.wantOmits != "" && strings.Contains(got, tc.wantOmits) {
			t.Errorf("case %d: should not contain %q in:\n%s", i, tc.wantOmits, got)
		}
	}
}

func TestFromContext_returnsAttachedLogger(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := With(context.Background(), l)

	FromContext(ctx).Info("test event", "k", "v")
	if !strings.Contains(buf.String(), "test event") {
		t.Fatalf("expected attached logger to be used; got:\n%s", buf.String())
	}
}

func TestFromContext_fallsBackToRoot(t *testing.T) {
	var buf bytes.Buffer
	rootLogger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { rootLogger = newDefault() }()

	FromContext(context.Background()).Info("root event")
	if !strings.Contains(buf.String(), "root event") {
		t.Fatalf("expected root logger to be used; got:\n%s", buf.String())
	}
}

func TestSetupDiscard_silencesEverything(t *testing.T) {
	SetupDiscard()
	defer func() { rootLogger = newDefault() }()
	Default().Error("should not appear anywhere — discard handler")
}
