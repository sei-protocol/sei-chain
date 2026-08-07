package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
)

// runMerge combines each shard's timings.shard-N.json into a single
// package_timings.json. Shards test disjoint package sets, so key
// collisions aren't expected; if one occurs, the last file wins.
func runMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	out := fs.String("out", "", "path to write the merged timing JSON to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs := fs.Args()
	if *out == "" || len(inputs) == 0 {
		return fmt.Errorf("--out and at least one input file are required")
	}

	merged := map[string]float64{}
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var timings map[string]float64
		if err := json.Unmarshal(data, &timings); err != nil {
			return fmt.Errorf("decoding %s: %w", path, err)
		}
		maps.Copy(merged, timings)
	}

	fmt.Fprintf(os.Stderr, "testsplit: merged %d package timings from %d shard files\n", len(merged), len(inputs))
	return writeTimings(*out, merged)
}
