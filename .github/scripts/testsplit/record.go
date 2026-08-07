package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// resultLine matches Go's own test summary lines, e.g.:
//
//	ok  	github.com/sei-protocol/sei-chain/x/evm	12.345s
//	FAIL	github.com/sei-protocol/sei-chain/x/evm	3.210s
//
// This is what `go test` already prints per package, so no extra flags
// (e.g. -json) are needed to recover per-package elapsed time.
var resultLine = regexp.MustCompile(`^(?:ok|FAIL)\s+(\S+)\s+([0-9]+(?:\.[0-9]+)?)s(?:\s|$)`)

func runRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	input := fs.String("input", "", "path to captured `go test` output")
	out := fs.String("out", "", "path to write this shard's timing JSON to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" || *out == "" {
		return fmt.Errorf("--input and --out are required")
	}

	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()

	timings := map[string]float64{}
	scanner := bufio.NewScanner(f)
	// Test output lines can be long (e.g. verbose failure dumps); grow the
	// buffer rather than truncating/erroring on long lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		m := resultLine.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		seconds, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		timings[m[1]] = seconds
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", *input, err)
	}

	return writeTimings(*out, timings)
}

func writeTimings(path string, timings map[string]float64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(timings)
}
