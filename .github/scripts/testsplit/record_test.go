package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRecordParsesResultLines(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test-output.txt")
	out := filepath.Join(dir, "timings.json")

	content := `=== RUN   TestFoo
--- PASS: TestFoo (0.00s)
ok  	github.com/sei-protocol/sei-chain/x/evm	12.345s
FAIL	github.com/sei-protocol/sei-chain/x/gov	3.5s
?   	github.com/sei-protocol/sei-chain/x/notest	[no test files]
`
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runRecord([]string{"--input=" + input, "--out=" + out}); err != nil {
		t.Fatalf("runRecord() error = %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var timings map[string]float64
	if err := json.Unmarshal(data, &timings); err != nil {
		t.Fatal(err)
	}

	want := map[string]float64{
		"github.com/sei-protocol/sei-chain/x/evm": 12.345,
		"github.com/sei-protocol/sei-chain/x/gov": 3.5,
	}
	if len(timings) != len(want) {
		t.Fatalf("timings = %v, want %v", timings, want)
	}
	for pkg, dur := range want {
		if got := timings[pkg]; got != dur {
			t.Errorf("timings[%q] = %v, want %v", pkg, got, dur)
		}
	}
	if _, ok := timings["github.com/sei-protocol/sei-chain/x/notest"]; ok {
		t.Error("expected packages with no test files to be excluded")
	}
}
