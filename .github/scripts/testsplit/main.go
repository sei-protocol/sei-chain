// Command testsplit splits the Go package list for the Race Detection CI
// job across N shards, balancing by historical per-package test duration
// when available and falling back to round-robin when it isn't.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testsplit <plan|record|merge> [flags]")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "plan":
		err = runPlan(os.Args[2:])
	case "record":
		err = runRecord(os.Args[2:])
	case "merge":
		err = runMerge(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "testsplit:", err)
		os.Exit(1)
	}
}
