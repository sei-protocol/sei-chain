// Regenerates precompiles/*/setup.go from each module's versions file.
// Triggered via: go generate ./...
//
// For each precompile module that has a "versions" file, this tool generates
// a setup.go that maps upgrade version strings to their legacy precompile
// constructors. The last entry in each versions file is the current/live
// version and is excluded from legacy imports (it uses the package-level
// NewPrecompile directly).
package main

import (
	"bufio"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed setup.go.tmpl
var tmplFS embed.FS

const precompilesDir = "precompiles"

var excludeDirs = map[string]bool{
	"common": true,
	"utils":  true,
}

type LegacyVersion struct {
	Tag    string // e.g. "v5.5.2"
	Folder string // e.g. "v552"
}

type TemplateData struct {
	PackageName    string
	LegacyVersions []LegacyVersion
}

func main() {
	tmpl, err := template.ParseFS(tmplFS, "setup.go.tmpl")
	if err != nil {
		fatalf("parsing template: %v", err)
	}

	entries, err := os.ReadDir(precompilesDir)
	if err != nil {
		fatalf("reading %s: %v", precompilesDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || excludeDirs[entry.Name()] {
			continue
		}

		moduleName := entry.Name()
		moduleDir := filepath.Join(precompilesDir, moduleName)

		versionsFile := filepath.Join(moduleDir, "versions")
		if _, err := os.Stat(versionsFile); os.IsNotExist(err) {
			continue
		}

		if _, err := os.Stat(filepath.Join(moduleDir, moduleName+".go")); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "skipping %s: no %s.go found\n", moduleName, moduleName)
			continue
		}

		versions, err := readVersions(versionsFile)
		if err != nil {
			fatalf("reading versions for %s: %v", moduleName, err)
		}
		if len(versions) == 0 {
			continue
		}

		// All entries except the last are legacy versions that get imported.
		// The last entry is the current/live version whose code lives in the
		// package root and is called via NewPrecompile(keepers) directly.
		legacy := make([]LegacyVersion, 0, len(versions)-1)
		for _, v := range versions[:len(versions)-1] {
			legacy = append(legacy, LegacyVersion{
				Tag:    v,
				Folder: versionFolder(v),
			})
		}

		if err := generateSetup(tmpl, moduleDir, moduleName, legacy); err != nil {
			fatalf("generating setup.go for %s: %v", moduleName, err)
		}

		fmt.Printf("generated %s/setup.go (%d legacy versions)\n", moduleDir, len(legacy))
	}
}

func readVersions(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var versions []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			versions = append(versions, line)
		}
	}
	return versions, scanner.Err()
}

// versionFolder converts a version tag to a folder/package name by removing
// dots. Two-part versions (v7.0) are padded to three-part (v7.0.0) first so
// that folder names sort consistently with existing ones (v630, v640, v700).
func versionFolder(v string) string {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) == 2 {
		v = v + ".0"
	}
	return strings.ReplaceAll(v, ".", "")
}

func generateSetup(tmpl *template.Template, moduleDir, moduleName string, legacy []LegacyVersion) error {
	data := TemplateData{
		PackageName:    moduleName,
		LegacyVersions: legacy,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return fmt.Errorf("gofmt: %w\nraw:\n%s", err, buf.String())
	}

	return os.WriteFile(filepath.Join(moduleDir, "setup.go"), formatted, 0644)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gensetup: "+format+"\n", args...)
	os.Exit(1)
}
