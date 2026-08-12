package main

// Command ipmt-parse parses IPMT files and outputs JSON.
//
// gl:docs/ipmt-parser.md

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

func main() {
	inFile := flag.String("in", "", "input .ipmt file (default: stdin)")
	pretty := flag.Bool("pretty", true, "pretty-print JSON output")
	version := cli.VersionFlag(flag.CommandLine, "ipmt-parse")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ipmt-parse [--in <file.ipmt>] [--pretty=true]")
		fmt.Fprintln(os.Stderr, "Reads from stdin when --in is omitted.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	if version(os.Stdout) {
		return
	}

	var data []byte
	var err error
	if *inFile == "" {
		data, err = io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fmt.Fprintln(os.Stderr, "read stdin:", err)
			os.Exit(2)
		}
	} else {
		data, err = os.ReadFile(*inFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read file:", err)
			os.Exit(2)
		}
	}

	doc, err := parser.ParseIPMTBytes(data, *inFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	var b []byte
	if *pretty {
		b, err = json.MarshalIndent(doc, "", "  ")
	} else {
		b, err = json.Marshal(doc)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode JSON:", err)
		os.Exit(2)
	}
	fmt.Println(string(b))
}
