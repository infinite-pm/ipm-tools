// Command ipm-rpc-tee is a recording passthrough for the language server:
// the editor talks to it, it talks to a real ipm-rpc, and every buffer state
// that crosses the wire is appended to a JSONL capture.
//
// Why a proxy rather than a flag in ipm-rpc: the shipping server carries no
// dev-only surface, the same way cmd/layout-gen carries no debug views
// (gl:docs/dev/layout-gen/layout-debug.md). Nothing dev-only can then end up
// in a Marketplace .vsix, and this tool can be as chatty as it likes.
//
// It exists because the demo recorder (vscode-infinite-pm-dev) types real
// ipmt into real VS Code, and every keystroke is a diagram state the layout
// engine either did lay out or would if asked. Those states are the one
// corpus of diagrams MID-CONSTRUCTION that the authored fixtures cannot
// contain — and they are already on this wire.
//
// Configuration is by environment, because an LSP client supplies the server
// PATH and no arguments (the extension passes `args: []`):
//
//	IPM_TEE_REAL=/path/to/ipm-rpc   the server to proxy      (required)
//	IPM_TEE_OUT=/path/to/dir        where captures are written (required)
//
// Flags --real/--out override them for direct use. Any other argument (the
// --stdio every LSP client appends) is passed through to the real server.
//
// The capture is best-effort by construction: a recording error is logged
// and dropped, never propagated. Breaking a recording session to save a
// corpus entry would be a bad trade.
//
// gl:docs/dev-tools/states-corpus.md
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creachadair/jrpc2/channel"
)

const toolName = "ipm-rpc-tee"

func main() {
	real, out, rest := parseArgs(os.Args[1:])
	if real == "" {
		real = os.Getenv("IPM_TEE_REAL")
	}
	if out == "" {
		out = os.Getenv("IPM_TEE_OUT")
	}
	if real == "" {
		usage()
		os.Exit(2)
	}
	rec, err := newRecorder(out)
	if err != nil {
		// A capture we cannot write must not stop the editor session: run as
		// a plain passthrough and say so.
		fmt.Fprintf(os.Stderr, "%s: capture disabled: %v\n", toolName, err)
	}
	defer rec.Close()

	if err := proxy(real, rest, rec); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", toolName, err)
		os.Exit(1)
	}
}

// parseArgs takes this tool's two options out of the command line and passes
// EVERYTHING ELSE through to the real server.
//
// Deliberately not the flag package: an LSP client appends its own arguments
// unconditionally (vscode-languageclient adds --stdio), and a strict parser
// exits with "flag provided but not defined" before the session even starts —
// which is a dead editor, not a missing capture. ipm-rpc accepts --stdio for
// exactly the same reason; a proxy in front of it has to be at least as
// forgiving as what it proxies.
func parseArgs(args []string) (real, out string, rest []string) {
	take := func(i int, name string) (string, int, bool) {
		a := args[i]
		if a == "--"+name || a == "-"+name {
			if i+1 < len(args) {
				return args[i+1], i + 1, true
			}
			return "", i, true
		}
		for _, pre := range []string{"--" + name + "=", "-" + name + "="} {
			if strings.HasPrefix(a, pre) {
				return strings.TrimPrefix(a, pre), i, true
			}
		}
		return "", i, false
	}
	for i := 0; i < len(args); i++ {
		if v, ni, ok := take(i, "real"); ok {
			real, i = v, ni
			continue
		}
		if v, ni, ok := take(i, "out"); ok {
			out, i = v, ni
			continue
		}
		if args[i] == "-h" || args[i] == "--help" || args[i] == "-help" {
			usage()
			os.Exit(0)
		}
		rest = append(rest, args[i])
	}
	return real, out, rest
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [--real <ipm-rpc>] [--out <dir>] [server args…]\n", toolName)
	fmt.Fprintln(os.Stderr, "\nRecording LSP passthrough: forwards stdio verbatim and appends every")
	fmt.Fprintln(os.Stderr, "buffer state to <out>/session-<pid>.jsonl. Unrecognised arguments")
	fmt.Fprintln(os.Stderr, "(--stdio and friends) are handed to the proxied server.")
	fmt.Fprintln(os.Stderr, "\n  --real  path to the ipm-rpc binary to proxy   (env IPM_TEE_REAL)")
	fmt.Fprintln(os.Stderr, "  --out   directory to append the capture to    (env IPM_TEE_OUT)")
}

// proxy runs the real server and shuttles framed messages both ways.
func proxy(real string, args []string, rec *recorder) error {
	cmd := exec.Command(real, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	in, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("server stdin: %w", err)
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("server stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", real, err)
	}

	client := channel.LSP(os.Stdin, os.Stdout) // editor ⇄ us
	server := channel.LSP(outPipe, in)         // us ⇄ real server

	var wg sync.WaitGroup
	wg.Add(2)
	// editor → server: the direction the states travel.
	go func() {
		defer wg.Done()
		pump(client, server, rec.fromClient)
		_ = in.Close() // EOF the server so it exits with us
	}()
	// server → editor: forwarded untouched; diagnostics are noted in passing.
	go func() {
		defer wg.Done()
		pump(server, client, rec.fromServer)
	}()
	wg.Wait()
	return cmd.Wait()
}

// pump forwards every record from src to dst, observing each one. FORWARD
// FIRST, observe after: the session's latency and correctness never depend on
// the recorder.
func pump(src, dst channel.Channel, observe func([]byte)) {
	for {
		msg, err := src.Recv()
		if err != nil {
			return
		}
		if err := dst.Send(msg); err != nil {
			return
		}
		observe(msg)
	}
}

// ---- capture ---------------------------------------------------------------

// state is one recorded line. The raw capture keeps timestamps and every
// duplicate; states-curate turns it into the deterministic corpus.
type state struct {
	Seq     int    `json:"seq"`
	TS      string `json:"ts"`
	Cadence string `json:"cadence"` // open | change | embed | embed-tokens | save | diagnostics
	URI     string `json:"uri"`
	Lang    string `json:"lang,omitempty"`
	Version int    `json:"version,omitempty"`
	Text    string `json:"text,omitempty"`
	// Diagnostics carries the server's verdict on the state that produced it —
	// which is how a capture knows WHICH states were invalid, and why, without
	// re-running anything.
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type recorder struct {
	mu  sync.Mutex
	dir string
	w   *os.File
	enc *json.Encoder
	seq int
	off bool // capture disabled (unwritable, or no directory)
}

func newRecorder(dir string) (*recorder, error) {
	if dir == "" {
		return &recorder{off: true}, errors.New("no output directory (set IPM_TEE_OUT or --out)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &recorder{off: true}, err
	}
	return &recorder{dir: dir}, nil
}

// open creates the capture file on the FIRST state, not at startup.
//
// The extension probes the server with --version before it starts a session,
// and a restart spawns another process; each of those reaches the tee and
// would otherwise leave an empty session file behind for the curator to sift.
// One file per proxy process that actually saw something, and the recorder
// never renames it: a scene launches its own VS Code, so one file IS one
// scene, and which scene is derivable from the URIs inside.
func (r *recorder) open() bool {
	if r.enc != nil {
		return true
	}
	if r.off || r.dir == "" {
		return false
	}
	path := filepath.Join(r.dir, fmt.Sprintf("session-%d.jsonl", os.Getpid()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: capture open: %v\n", toolName, err)
		r.off = true
		return false
	}
	fmt.Fprintf(os.Stderr, "%s: capturing to %s\n", toolName, path)
	r.w, r.enc = f, json.NewEncoder(f)
	return true
}

func (r *recorder) Close() {
	if r == nil || r.w == nil {
		return
	}
	_ = r.w.Close()
}

func (r *recorder) write(s state) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.open() {
		return
	}
	r.seq++
	s.Seq = r.seq
	s.TS = time.Now().UTC().Format(time.RFC3339Nano)
	if err := r.enc.Encode(s); err != nil {
		fmt.Fprintf(os.Stderr, "%s: capture write: %v\n", toolName, err)
		r.off = true // stop trying; the session continues either way
	}
}

// lspMessage is the subset of the protocol this tool reads. Everything else
// passes through unexamined.
type lspMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (r *recorder) fromClient(msg []byte) {
	var m lspMessage
	if json.Unmarshal(msg, &m) != nil || m.Method == "" {
		return
	}
	switch m.Method {
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI        string `json:"uri"`
				LanguageID string `json:"languageId"`
				Version    int    `json:"version"`
				Text       string `json:"text"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			r.write(state{Cadence: "open", URI: p.TextDocument.URI, Lang: p.TextDocument.LanguageID,
				Version: p.TextDocument.Version, Text: p.TextDocument.Text})
		}
	case "textDocument/didChange":
		// The server advertises sync=Full, so a change carries the WHOLE
		// buffer — every keystroke is a complete, replayable state, which is
		// what makes this capture a corpus rather than a patch log.
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int    `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if json.Unmarshal(m.Params, &p) == nil && len(p.ContentChanges) > 0 {
			r.write(state{Cadence: "change", URI: p.TextDocument.URI,
				Version: p.TextDocument.Version, Text: p.ContentChanges[len(p.ContentChanges)-1].Text})
		}
	case "workspace/executeCommand":
		var p struct {
			Command   string            `json:"command"`
			Arguments []json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &p) != nil {
			return
		}
		var arg struct {
			URI        string `json:"uri"`
			TokensOnly bool   `json:"tokensOnly"`
		}
		if len(p.Arguments) > 0 {
			_ = json.Unmarshal(p.Arguments[0], &arg)
		}
		// These mark WHICH states the engine actually laid out, as opposed to
		// which the user saw: ipm.embedBuffer with tokensOnly skips the
		// layout, a full one runs it, ipm.embed writes to disk.
		switch p.Command {
		case "ipm.embedBuffer":
			cadence := "embed"
			if arg.TokensOnly {
				cadence = "embed-tokens"
			}
			r.write(state{Cadence: cadence, URI: arg.URI})
		case "ipm.embed":
			r.write(state{Cadence: "save", URI: arg.URI})
		}
	}
}

func (r *recorder) fromServer(msg []byte) {
	var m lspMessage
	if json.Unmarshal(msg, &m) != nil || m.Method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return
	}
	var msgs []string
	for _, d := range p.Diagnostics {
		msgs = append(msgs, strings.TrimSpace(d.Message))
	}
	r.write(state{Cadence: "diagnostics", URI: p.URI, Diagnostics: msgs})
}
