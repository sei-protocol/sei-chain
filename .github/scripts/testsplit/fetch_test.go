package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHub serves just enough of the Actions API for fetchTimings: a
// workflow-runs list (branch -> run ID) and an artifacts list + zip body
// for whichever branches have a "package-timings" artifact.
func fakeGitHub(t *testing.T, branchesWithHistory map[string]map[string]float64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/o/r/actions/workflows/go-test.yml/runs", func(w http.ResponseWriter, r *http.Request) {
		branch := r.URL.Query().Get("branch")
		if _, ok := branchesWithHistory[branch]; !ok {
			fmt.Fprint(w, `{"workflow_runs": []}`)
			return
		}
		fmt.Fprintf(w, `{"workflow_runs": [{"id": %d}]}`, branchRunID(branch))
	})

	mux.HandleFunc("/repos/o/r/actions/runs/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			fmt.Fprintf(w, `{"artifacts": [{"name": "package-timings", "archive_download_url": %q, "expired": false}]}`,
				"http://"+r.Host+"/download/"+strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/o/r/actions/runs/"), "/artifacts"))
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		runIDStr := strings.TrimPrefix(r.URL.Path, "/download/")
		var branch string
		for b := range branchesWithHistory {
			if fmt.Sprint(branchRunID(b)) == runIDStr {
				branch = b
			}
		}
		timings := branchesWithHistory[branch]
		data, err := json.Marshal(timings)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, err := zw.Create("package_timings.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		w.Write(buf.Bytes())
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func branchRunID(branch string) int {
	sum := 0
	for _, c := range branch {
		sum += int(c)
	}
	return sum
}

func TestFetchTimingsPrefersOwnBranch(t *testing.T) {
	srv := fakeGitHub(t, map[string]map[string]float64{
		"my-feature": {"pkgA": 1},
		"main":       {"pkgA": 2},
	})
	restoreAPIBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = restoreAPIBase })

	timings, source, err := fetchTimings("o/r", "go-test.yml", "my-feature", "main", "tok")
	if err != nil {
		t.Fatalf("fetchTimings() error = %v", err)
	}
	if source != "my-feature" {
		t.Errorf("source = %q, want %q", source, "my-feature")
	}
	if timings["pkgA"] != 1 {
		t.Errorf("timings[pkgA] = %v, want 1 (own-branch data, not main's)", timings["pkgA"])
	}
}

func TestFetchTimingsFallsBackToBaseBranch(t *testing.T) {
	srv := fakeGitHub(t, map[string]map[string]float64{
		// "my-feature" has no history yet (e.g. first push on this PR).
		"main": {"pkgA": 2},
	})
	restoreAPIBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = restoreAPIBase })

	timings, source, err := fetchTimings("o/r", "go-test.yml", "my-feature", "main", "tok")
	if err != nil {
		t.Fatalf("fetchTimings() error = %v", err)
	}
	if source != "main" {
		t.Errorf("source = %q, want %q", source, "main")
	}
	if timings["pkgA"] != 2 {
		t.Errorf("timings[pkgA] = %v, want 2 (base-branch data)", timings["pkgA"])
	}
}

func TestFetchTimingsNoHistoryAnywhere(t *testing.T) {
	srv := fakeGitHub(t, map[string]map[string]float64{})
	restoreAPIBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = restoreAPIBase })

	_, _, err := fetchTimings("o/r", "go-test.yml", "my-feature", "main", "tok")
	if err == nil {
		t.Fatal("fetchTimings() expected an error when no branch has history, got nil")
	}
}

func TestFetchTimingsSameBranchAsBase(t *testing.T) {
	// A push-triggered run on main: branch == baseBranch, must not double-query.
	srv := fakeGitHub(t, map[string]map[string]float64{
		"main": {"pkgA": 2},
	})
	restoreAPIBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = restoreAPIBase })

	timings, source, err := fetchTimings("o/r", "go-test.yml", "main", "main", "tok")
	if err != nil {
		t.Fatalf("fetchTimings() error = %v", err)
	}
	if source != "main" || timings["pkgA"] != 2 {
		t.Errorf("got source=%q timings=%v, want source=main timings[pkgA]=2", source, timings)
	}
}
