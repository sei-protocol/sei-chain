package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// minTimingCoverage is the fraction of packages that must have a known
// duration before we trust bin-packing over round-robin. Below this, too
// many durations would be guesses and bin-packing wouldn't out-perform a
// plain round-robin split.
const minTimingCoverage = 0.5

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	numSplit := fs.Int("num-split", 0, "number of shards to split packages into")
	outDir := fs.String("out-dir", "build", "directory to write packages.txt.N shard files into")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/repo to query for prior timing data")
	workflow := fs.String("workflow", "go-test.yml", "workflow file name to query for the last successful run")
	branch := fs.String("branch", "main", "branch to source prior timing data from")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *numSplit < 1 {
		return fmt.Errorf("--num-split must be >= 1, got %d", *numSplit)
	}

	packages, err := readPackages(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading package list: %w", err)
	}
	if len(packages) == 0 {
		return fmt.Errorf("no packages given on stdin")
	}

	var shards [][]string
	timings, err := fetchTimings(*repo, *workflow, *branch, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "testsplit: could not fetch prior timings, falling back to round-robin: %v\n", err)
		shards = roundRobin(packages, *numSplit)
	} else if coverage := timingCoverage(packages, timings); coverage < minTimingCoverage {
		fmt.Fprintf(os.Stderr, "testsplit: only %.0f%% of packages have known timings, falling back to round-robin\n", coverage*100)
		shards = roundRobin(packages, *numSplit)
	} else {
		fmt.Fprintf(os.Stderr, "testsplit: bin-packing %d packages across %d shards using historical timings\n", len(packages), *numSplit)
		shards = binPack(packages, timings, *numSplit)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	for i, shard := range shards {
		path := filepath.Join(*outDir, fmt.Sprintf("packages.txt.%d", i))
		var buf bytes.Buffer
		for _, pkg := range shard {
			buf.WriteString(pkg)
			buf.WriteByte('\n')
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func readPackages(r io.Reader) ([]string, error) {
	var packages []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		packages = append(packages, line)
	}
	return packages, scanner.Err()
}

func timingCoverage(packages []string, timings map[string]float64) float64 {
	if len(packages) == 0 {
		return 0
	}
	known := 0
	for _, pkg := range packages {
		if _, ok := timings[pkg]; ok {
			known++
		}
	}
	return float64(known) / float64(len(packages))
}

// roundRobin deterministically interleaves packages across shards. It
// requires no historical data and, unlike a contiguous chunk split, avoids
// dumping a whole cluster of alphabetically-adjacent (and often
// runtime-correlated, e.g. a module's many keeper packages) packages into
// a single shard.
func roundRobin(packages []string, numSplit int) [][]string {
	shards := make([][]string, numSplit)
	for i, pkg := range packages {
		idx := i % numSplit
		shards[idx] = append(shards[idx], pkg)
	}
	return shards
}

// binPack assigns packages to shards using longest-processing-time-first
// greedy scheduling: process packages slowest-first, each time adding the
// package to whichever shard currently has the smallest total duration.
// Packages with no known duration are estimated at the mean of known
// durations so a handful of unknowns can't skew the packing.
func binPack(packages []string, timings map[string]float64, numSplit int) [][]string {
	mean := meanDuration(timings)

	type pkgDuration struct {
		pkg string
		dur float64
	}
	durations := make([]pkgDuration, 0, len(packages))
	for _, pkg := range packages {
		dur, ok := timings[pkg]
		if !ok {
			dur = mean
		}
		durations = append(durations, pkgDuration{pkg: pkg, dur: dur})
	}
	sort.SliceStable(durations, func(i, j int) bool {
		return durations[i].dur > durations[j].dur
	})

	shards := make([][]string, numSplit)
	totals := make([]float64, numSplit)
	for _, pd := range durations {
		lightest := 0
		for i := 1; i < numSplit; i++ {
			if totals[i] < totals[lightest] {
				lightest = i
			}
		}
		shards[lightest] = append(shards[lightest], pd.pkg)
		totals[lightest] += pd.dur
	}
	return shards
}

func meanDuration(timings map[string]float64) float64 {
	if len(timings) == 0 {
		return 0
	}
	var total float64
	for _, dur := range timings {
		total += dur
	}
	return total / float64(len(timings))
}

// fetchTimings finds the most recent successful push-triggered run of
// `workflow` on `branch` and downloads the per-package timing data it
// recorded, if any. It intentionally only looks at `branch` runs so a
// noisy or unusually slow PR branch can never skew another PR's shard
// assignment.
func fetchTimings(repo, workflow, branch, token string) (map[string]float64, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is empty (set --repo or GITHUB_REPOSITORY)")
	}
	if token == "" {
		return nil, fmt.Errorf("no GITHUB_TOKEN provided")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	runID, err := latestSuccessfulRun(client, repo, workflow, branch, token)
	if err != nil {
		return nil, err
	}

	artifactURL, err := findTimingsArtifactURL(client, repo, runID, token)
	if err != nil {
		return nil, err
	}

	return downloadTimingsArtifact(client, artifactURL, token)
}

func apiGet(client *http.Client, url, token string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sei-chain-testsplit")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: unexpected status %s: %s", url, resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func latestSuccessfulRun(client *http.Client, repo, workflow, branch, token string) (int64, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/workflows/%s/runs?branch=%s&status=success&event=push&per_page=1",
		repo, workflow, branch,
	)
	var result struct {
		WorkflowRuns []struct {
			ID int64 `json:"id"`
		} `json:"workflow_runs"`
	}
	if err := apiGet(client, url, token, &result); err != nil {
		return 0, err
	}
	if len(result.WorkflowRuns) == 0 {
		return 0, fmt.Errorf("no successful %s runs found on %s", workflow, branch)
	}
	return result.WorkflowRuns[0].ID, nil
}

const timingsArtifactName = "package-timings"

func findTimingsArtifactURL(client *http.Client, repo string, runID int64, token string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d/artifacts", repo, runID)
	var result struct {
		Artifacts []struct {
			Name               string `json:"name"`
			ArchiveDownloadURL string `json:"archive_download_url"`
			Expired            bool   `json:"expired"`
		} `json:"artifacts"`
	}
	if err := apiGet(client, url, token, &result); err != nil {
		return "", err
	}
	for _, a := range result.Artifacts {
		if a.Name == timingsArtifactName && !a.Expired {
			return a.ArchiveDownloadURL, nil
		}
	}
	return "", fmt.Errorf("no %q artifact found on run %d", timingsArtifactName, runID)
}

const timingsFileName = "package_timings.json"

func downloadTimingsArtifact(client *http.Client, url, token string) (map[string]float64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "sei-chain-testsplit")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading artifact: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("artifact is not a valid zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != timingsFileName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		var timings map[string]float64
		if err := json.NewDecoder(rc).Decode(&timings); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", timingsFileName, err)
		}
		return timings, nil
	}
	return nil, fmt.Errorf("%s not found in artifact zip", timingsFileName)
}
