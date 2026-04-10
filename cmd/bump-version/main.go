// Archives current precompile code into legacy/ folders and updates version
// tracking files. This replaces the old scripts/bump-version.sh.
//
// Usage:
//
//	go run ./cmd/bump-version <NEW_TAG> --modules bank,wasmd,...
//	go run ./cmd/bump-version <NEW_TAG> --all
//
// After running this, execute `go generate ./...` to regenerate setup.go files.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	tagFile        = "app/tags"
	precompilesDir = "precompiles"
	commonDir      = "precompiles/common"
	modulePath     = "github.com/sei-protocol/sei-chain"
)

var (
	excludeDirs  = map[string]bool{"common": true, "utils": true}
	packageRe    = regexp.MustCompile(`^package\s+\S+`)
	commonImport = `"` + modulePath + `/precompiles/common"`
)

func main() {
	args := parseArgs()

	// Validate tag file exists
	if _, err := os.Stat(tagFile); os.IsNotExist(err) {
		fatalf("tag file not found: %s", tagFile)
	}

	// Dedup on rerun: if NEW_TAG is already the last line in app/tags, remove it
	dedup(tagFile, args.newTag)

	tagFolder := strings.ReplaceAll(args.newTag, ".", "")
	legacyCommonImport := `"` + modulePath + `/precompiles/common/legacy/` + tagFolder + `"`

	// Resolve which modules to archive
	modules := args.modules
	if args.all {
		modules = discoverModules()
	}
	if len(modules) == 0 {
		fatalf("no modules to archive; use --modules or --all")
	}

	fmt.Printf("new tag: %s\n", args.newTag)
	fmt.Printf("version folder: %s\n", tagFolder)
	fmt.Printf("modules: %s\n", strings.Join(modules, ", "))

	// Always archive common when any module is archived
	archiveCommon(tagFolder)

	// Archive each module
	for _, mod := range modules {
		moduleDir := filepath.Join(precompilesDir, mod)
		if _, err := os.Stat(moduleDir); os.IsNotExist(err) {
			fatalf("module directory not found: %s", moduleDir)
		}
		archiveModule(moduleDir, mod, args.newTag, tagFolder, legacyCommonImport)
	}

	// Append new tag to app/tags
	appendLine(tagFile, args.newTag)
	fmt.Printf("appended %s to %s\n", args.newTag, tagFile)

	fmt.Println("\nnext step: run `go generate ./...` to regenerate setup.go files")
}

type cliArgs struct {
	newTag  string
	modules []string
	all     bool
}

func parseArgs() cliArgs {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}

	result := cliArgs{newTag: args[0]}
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--modules":
			i++
			if i >= len(args) {
				fatalf("--modules requires a comma-separated list")
			}
			for _, m := range strings.Split(args[i], ",") {
				m = strings.TrimSpace(m)
				if m != "" {
					result.modules = append(result.modules, m)
				}
			}
		case "--all":
			result.all = true
		default:
			fatalf("unknown argument: %s", args[i])
		}
		i++
	}

	if !result.all && len(result.modules) == 0 {
		usage()
	}
	return result
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: go run ./cmd/bump-version <NEW_TAG> --modules bank,wasmd,...\n")
	fmt.Fprintf(os.Stderr, "       go run ./cmd/bump-version <NEW_TAG> --all\n")
	os.Exit(1)
}

// discoverModules returns all precompile module names (excluding common, utils).
func discoverModules() []string {
	entries, err := os.ReadDir(precompilesDir)
	if err != nil {
		fatalf("reading %s: %v", precompilesDir, err)
	}
	var modules []string
	for _, e := range entries {
		if e.IsDir() && !excludeDirs[e.Name()] {
			modules = append(modules, e.Name())
		}
	}
	return modules
}

// archiveCommon copies precompiles.go and evm_events.go into common/legacy/{tagFolder}/.
func archiveCommon(tagFolder string) {
	targetDir := filepath.Join(commonDir, "legacy", tagFolder)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		fatalf("creating %s: %v", targetDir, err)
	}

	for _, name := range []string{"precompiles.go", "evm_events.go"} {
		src := filepath.Join(commonDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		dst := filepath.Join(targetDir, name)
		if err := copyAndRewritePackage(src, dst, tagFolder); err != nil {
			fatalf("archiving %s: %v", src, err)
		}
	}

	fmt.Printf("archived common -> legacy/%s/\n", tagFolder)
}

// archiveModule copies .go files and abi.json into {module}/legacy/{tagFolder}/,
// rewrites package declarations and common imports, and updates the versions file.
func archiveModule(moduleDir, moduleName, newTag, tagFolder, legacyCommonImport string) {
	targetDir := filepath.Join(moduleDir, "legacy", tagFolder)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		fatalf("creating %s: %v", targetDir, err)
	}

	// Find .go files to copy (exclude _test.go and setup.go)
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		fatalf("reading %s: %v", moduleDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") || name == "setup.go" {
			continue
		}

		src := filepath.Join(moduleDir, name)
		dst := filepath.Join(targetDir, name)
		if err := copyRewritePackageAndImports(src, dst, tagFolder, legacyCommonImport); err != nil {
			fatalf("archiving %s: %v", src, err)
		}
	}

	// Copy abi.json if present
	abiSrc := filepath.Join(moduleDir, "abi.json")
	if _, err := os.Stat(abiSrc); err == nil {
		if err := copyFile(abiSrc, filepath.Join(targetDir, "abi.json")); err != nil {
			fatalf("copying abi.json for %s: %v", moduleName, err)
		}
	}

	// Update versions file
	versionsFile := filepath.Join(moduleDir, "versions")
	dedup(versionsFile, newTag)
	appendLine(versionsFile, newTag)

	fmt.Printf("archived %s -> legacy/%s/\n", moduleName, tagFolder)
}

// copyAndRewritePackage copies a file and rewrites its package declaration.
func copyAndRewritePackage(src, dst, newPkg string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	rewritten := packageRe.ReplaceAllString(string(content), "package "+newPkg)
	return os.WriteFile(dst, []byte(rewritten), 0644)
}

// copyRewritePackageAndImports copies a file, rewrites its package declaration,
// and rewrites precompiles/common imports to point at the legacy snapshot.
func copyRewritePackageAndImports(src, dst, newPkg, legacyCommonImport string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	rewritten := packageRe.ReplaceAllString(string(content), "package "+newPkg)
	rewritten = strings.ReplaceAll(rewritten, commonImport, legacyCommonImport)
	return os.WriteFile(dst, []byte(rewritten), 0644)
}

// copyFile does a plain file copy.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// dedup removes the last line of a file if it matches the given value.
// This allows safe reruns of the tool.
func dedup(path, value string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	lines, err := readLines(path)
	if err != nil || len(lines) == 0 {
		return
	}
	if lines[len(lines)-1] == value {
		writeLines(path, lines[:len(lines)-1])
	}
}

// appendLine appends a line to a file (creating it if needed).
func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func writeLines(path string, lines []string) {
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fatalf("writing %s: %v", path, err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bump-version: "+format+"\n", args...)
	os.Exit(1)
}
