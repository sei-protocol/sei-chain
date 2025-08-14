// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

import (
	"bytes"
	"errors"
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sirkon/goproxy/internal/base"
	"github.com/sirkon/goproxy/internal/cfg"
	"github.com/sirkon/goproxy/internal/imports"
	"github.com/sirkon/goproxy/internal/modfetch"
	"github.com/sirkon/goproxy/internal/modfile"
	"github.com/sirkon/goproxy/internal/module"
	"github.com/sirkon/goproxy/internal/mvs"
	"github.com/sirkon/goproxy/internal/par"
	"github.com/sirkon/goproxy/internal/search"
	"github.com/sirkon/goproxy/internal/semver"
	"github.com/sirkon/goproxy/internal/str"
)

// buildList is the list of modules to use for building packages.
// It is initialized by calling ImportPaths, ImportFromFiles,
// LoadALL, or LoadBuildList, each of which uses loaded.load.
//
// Ideally, exactly ONE of those functions would be called,
// and exactly once. Most of the time, that's true.
// During "go get" it may not be. TODO(rsc): Figure out if
// that restriction can be established, or else document why not.
//
var buildList []module.Version

// loaded is the most recently-used package loader.
// It holds details about individual packages.
//
// Note that loaded.buildList is only valid during a load operation;
// afterward, it is copied back into the global buildList,
// which should be used instead.
var loaded *loader

// ImportPaths returns the set of packages matching the args (patterns),
// adding modules to the build list as needed to satisfy new imports.
func ImportPaths(patterns []string) []*search.Match {
	InitMod()

	var matches []*search.Match
	for _, pattern := range search.CleanPatterns(patterns) {
		m := &search.Match{
			Pattern: pattern,
			Literal: !strings.Contains(pattern, "...") && !search.IsMetaPackage(pattern),
		}
		if m.Literal {
			m.Pkgs = []string{pattern}
		}
		matches = append(matches, m)
	}

	fsDirs := make([][]string, len(matches))
	loaded = newLoader()
	updateMatches := func(iterating bool) {
		for i, m := range matches {
			switch {
			case build.IsLocalImport(m.Pattern) || filepath.IsAbs(m.Pattern):
				// Evaluate list of file system directories on first iteration.
				if fsDirs[i] == nil {
					var dirs []string
					if m.Literal {
						dirs = []string{m.Pattern}
					} else {
						dirs = search.MatchPackagesInFS(m.Pattern).Pkgs
					}
					fsDirs[i] = dirs
				}

				// Make a copy of the directory list and translate to import paths.
				// Note that whether a directory corresponds to an import path
				// changes as the build list is updated, and a directory can change
				// from not being in the build list to being in it and back as
				// the exact version of a particular module increases during
				// the loader iterations.
				m.Pkgs = str.StringList(fsDirs[i])
				for i, pkg := range m.Pkgs {
					dir := pkg
					if !filepath.IsAbs(dir) {
						dir = filepath.Join(cwd, pkg)
					} else {
						dir = filepath.Clean(dir)
					}

					// Note: The checks for @ here are just to avoid misinterpreting
					// the module cache directories (formerly GOPATH/src/mod/foo@v1.5.2/bar).
					// It's not strictly necessary but helpful to keep the checks.
					if dir == ModRoot {
						pkg = Target.Path
					} else if strings.HasPrefix(dir, ModRoot+string(filepath.Separator)) && !strings.Contains(dir[len(ModRoot):], "@") {
						suffix := filepath.ToSlash(dir[len(ModRoot):])
						if strings.HasPrefix(suffix, "/vendor/") {
							// TODO getmode vendor check
							pkg = strings.TrimPrefix(suffix, "/vendor/")
						} else {
							pkg = Target.Path + suffix
						}
					} else if sub := search.InDir(dir, cfg.GOROOTsrc); sub != "" && !strings.Contains(sub, "@") {
						pkg = filepath.ToSlash(sub)
					} else if path := pathInModuleCache(dir); path != "" {
						pkg = path
					} else {
						pkg = ""
						if !iterating {
							base.Errorf("go: directory %s outside available modules", base.ShortPath(dir))
						}
					}
					info, err := os.Stat(dir)
					if err != nil || !info.IsDir() {
						// If the directory does not exist,
						// don't turn it into an import path
						// that will trigger a lookup.
						pkg = ""
						if !iterating {
							if err != nil {
								base.Errorf("go: no such directory %v", m.Pattern)
							} else {
								base.Errorf("go: %s is not a directory", m.Pattern)
							}
						}
					}
					m.Pkgs[i] = pkg
				}

			case strings.Contains(m.Pattern, "..."):
				m.Pkgs = matchPackages(m.Pattern, loaded.tags, true, buildList)

			case m.Pattern == "all":
				loaded.testAll = true
				if iterating {
					// Enumerate the packages in the main module.
					// We'll load the dependencies as we find them.
					m.Pkgs = matchPackages("...", loaded.tags, false, []module.Version{Target})
				} else {
					// Starting with the packages in the main module,
					// enumerate the full list of "all".
					m.Pkgs = loaded.computePatternAll(m.Pkgs)
				}

			case search.IsMetaPackage(m.Pattern): // std, cmd
				if len(m.Pkgs) == 0 {
					m.Pkgs = search.MatchPackages(m.Pattern).Pkgs
				}
			}
		}
	}

	loaded.load(func() []string {
		var roots []string
		updateMatches(true)
		for _, m := range matches {
			for _, pkg := range m.Pkgs {
				if pkg != "" {
					roots = append(roots, pkg)
				}
			}
		}
		return roots
	})

	// One last pass to finalize wildcards.
	updateMatches(false)

	// A given module path may be used as itself or as a replacement for another
	// module, but not both at the same time. Otherwise, the aliasing behavior is
	// too subtle (see https://golang.org/issue/26607), and we don't want to
	// commit to a specific behavior at this point.
	firstPath := make(map[module.Version]string, len(buildList))
	for _, mod := range buildList {
		src := mod
		if rep := Replacement(mod); rep.Path != "" {
			src = rep
		}
		if prev, ok := firstPath[src]; !ok {
			firstPath[src] = mod.Path
		} else if prev != mod.Path {
			base.Errorf("go: %s@%s used for two different module paths (%s and %s)", src.Path, src.Version, prev, mod.Path)
		}
	}
	base.ExitIfErrors()
	WriteGoMod()

	search.WarnUnmatched(matches)
	return matches
}

// pathInModuleCache returns the import path of the directory dir,
// if dir is in the module cache copy of a module in our build list.
func pathInModuleCache(dir string) string {
	for _, m := range buildList[1:] {
		root, err := modfetch.DownloadDir(m)
		if err != nil {
			continue
		}
		if sub := search.InDir(dir, root); sub != "" {
			sub = filepath.ToSlash(sub)
			if !strings.Contains(sub, "/vendor/") && !strings.HasPrefix(sub, "vendor/") && !strings.Contains(sub, "@") {
				return path.Join(m.Path, filepath.ToSlash(sub))
			}
		}
	}
	return ""
}

// warnPattern returns list, the result of matching pattern,
// but if list is empty then first it prints a warning about
// the pattern not matching any packages.
func warnPattern(pattern string, list []string) []string {
	if len(list) == 0 {
		fmt.Fprintf(os.Stderr, "warning: %q matched no packages\n", pattern)
	}
	return list
}

// ImportFromFiles adds modules to the build list as needed
// to satisfy the imports in the named Go source files.
func ImportFromFiles(gofiles []string) {
	InitMod()

	imports, testImports, err := imports.ScanFiles(gofiles, imports.Tags())
	if err != nil {
		base.Fatalf("go: %v", err)
	}

	loaded = newLoader()
	loaded.load(func() []string {
		var roots []string
		roots = append(roots, imports...)
		roots = append(roots, testImports...)
		return roots
	})
	WriteGoMod()
}

// DirImportPath returns the effective import path for dir,
// provided it is within the main module, or else returns ".".
func DirImportPath(dir string) string {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	} else {
		dir = filepath.Clean(dir)
	}

	if dir == ModRoot {
		return Target.Path
	}
	if strings.HasPrefix(dir, ModRoot+string(filepath.Separator)) {
		suffix := filepath.ToSlash(dir[len(ModRoot):])
		if strings.HasPrefix(suffix, "/vendor/") {
			return strings.TrimPrefix(suffix, "/vendor/")
		}
		return Target.Path + suffix
	}
	return "."
}

// LoadBuildList loads and returns the build list from go.mod.
// The loading of the build list happens automatically in ImportPaths:
// LoadBuildList need only be called if ImportPaths is not
// (typically in commands that care about the module but
// no particular package).
func LoadBuildList() []module.Version {
	InitMod()
	ReloadBuildList()
	WriteGoMod()
	return buildList
}

func ReloadBuildList() []module.Version {
	loaded = newLoader()
	loaded.load(func() []string { return nil })
	return buildList
}

// LoadALL returns the set of all packages in the current module
// and their dependencies in any other modules, without filtering
// due to build tags, except "+build ignore".
// It adds modules to the build list as needed to satisfy new imports.
// This set is useful for deciding whether a particular import is needed
// anywhere in a module.
func LoadALL() []string {
	return loadAll(true)
}

// LoadVendor is like LoadALL but only follows test dependencies
// for tests in the main module. Tests in dependency modules are
// ignored completely.
// This set is useful for identifying the which packages to include in a vendor directory.
func LoadVendor() []string {
	return loadAll(false)
}

func loadAll(testAll bool) []string {
	InitMod()

	loaded = newLoader()
	loaded.isALL = true
	loaded.tags = anyTags
	loaded.testAll = testAll
	if !testAll {
		loaded.testRoots = true
	}
	all := TargetPackages()
	loaded.load(func() []string { return all })
	WriteGoMod()

	var paths []string
	for _, pkg := range loaded.pkgs {
		if e, ok := pkg.err.(*ImportMissingError); ok && e.Module.Path == "" {
			continue // Package doesn't actually exist.
		}
		paths = append(paths, pkg.path)
	}
	return paths
}

// anyTags is a special tags map that satisfies nearly all build tag expressions.
// Only "ignore" and malformed build tag requirements are considered false.
var anyTags = map[string]bool{"*": true}

// TargetPackages returns the list of packages in the target (top-level) module,
// under all build tag settings.
func TargetPackages() []string {
	return matchPackages("...", anyTags, false, []module.Version{Target})
}

// BuildList returns the module build list,
// typically constructed by a previous call to
// LoadBuildList or ImportPaths.
// The caller must not modify the returned list.
func BuildList() []module.Version {
	return buildList
}

// SetBuildList sets the module build list.
// The caller is responsible for ensuring that the list is valid.
// SetBuildList does not retain a reference to the original list.
func SetBuildList(list []module.Version) {
	buildList = append([]module.Version{}, list...)
}

// ImportMap returns the actual package import path
// for an import path found in source code.
// If the given import path does not appear in the source code
// for the packages that have been loaded, ImportMap returns the empty string.
func ImportMap(path string) string {
	pkg, ok := loaded.pkgCache.Get(path).(*loadPkg)
	if !ok {
		return ""
	}
	return pkg.path
}

// PackageDir returns the directory containing the source code
// for the package named by the import path.
func PackageDir(path string) string {
	pkg, ok := loaded.pkgCache.Get(path).(*loadPkg)
	if !ok {
		return ""
	}
	return pkg.dir
}

// PackageModule returns the module providing the package named by the import path.
func PackageModule(path string) module.Version {
	pkg, ok := loaded.pkgCache.Get(path).(*loadPkg)
	if !ok {
		return module.Version{}
	}
	return pkg.mod
}

// ModuleUsedDirectly reports whether the main module directly imports
// some package in the module with the given path.
func ModuleUsedDirectly(path string) bool {
	return loaded.direct[path]
}

// Lookup returns the source directory, import path, and any loading error for
// the package at path.
// Lookup requires that one of the Load functions in this package has already
// been called.
func Lookup(path string) (dir, realPath string, err error) {
	pkg, ok := loaded.pkgCache.Get(path).(*loadPkg)
	if !ok {
		// The loader should have found all the relevant paths.
		// There are a few exceptions, though:
		//	- during go list without -test, the p.Resolve calls to process p.TestImports and p.XTestImports
		//	  end up here to canonicalize the import paths.
		//	- during any load, non-loaded packages like "unsafe" end up here.
		//	- during any load, build-injected dependencies like "runtime/cgo" end up here.
		//	- because we ignore appengine/* in the module loader,
		//	  the dependencies of any actual appengine/* library end up here.
		dir := findStandardImportPath(path)
		if dir != "" {
			return dir, path, nil
		}
		return "", "", errMissing
	}
	return pkg.dir, pkg.path, pkg.err
}

// A loader manages the process of loading information about
// the required packages for a particular build,
// checking that the packages are available in the module set,
// and updating the module set if needed.
// Loading is an iterative process: try to load all the needed packages,
// but if imports are missing, try to resolve those imports, and repeat.
//
// Although most of the loading state is maintained in the loader struct,
// one key piece - the build list - is a global, so that it can be modified
// separate from the loading operation, such as during "go get"
// upgrades/downgrades or in "go mod" operations.
// TODO(rsc): It might be nice to make the loader take and return
// a buildList rather than hard-coding use of the global.
type loader struct {
	tags      map[string]bool // tags for scanDir
	testRoots bool            // include tests for roots
	isALL     bool            // created with LoadALL
	testAll   bool            // include tests for all packages

	// reset on each iteration
	roots    []*loadPkg
	pkgs     []*loadPkg
	work     *par.Work  // current work queue
	pkgCache *par.Cache // map from string to *loadPkg

	// computed at end of iterations
	direct    map[string]bool   // imported directly by main module
	goVersion map[string]string // go version recorded in each module
}

// LoadTests controls whether the loaders load tests of the root packages.
var LoadTests bool

func newLoader() *loader {
	ld := new(loader)
	ld.tags = imports.Tags()
	ld.testRoots = LoadTests
	return ld
}

func (ld *loader) reset() {
	ld.roots = nil
	ld.pkgs = nil
	ld.work = new(par.Work)
	ld.pkgCache = new(par.Cache)
}

// A loadPkg records information about a single loaded package.
type loadPkg struct {
	path        string         // import path
	mod         module.Version // module providing package
	dir         string         // directory containing source code
	imports     []*loadPkg     // packages imported by this one
	err         error          // error loading package
	stack       *loadPkg       // package importing this one in minimal import stack for this pkg
	test        *loadPkg       // package with test imports, if we need test
	testOf      *loadPkg
	testImports []string // test-only imports, saved for use by pkg.test.
}

var errMissing = errors.New("cannot find package")

// load attempts to load the build graph needed to process a set of root packages.
// The set of root packages is defined by the addRoots function,
// which must call add(path) with the import path of each root package.
func (ld *loader) load(roots func() []string) {
	var err error
	reqs := Reqs()
	buildList, err = mvs.BuildList(Target, reqs)
	if err != nil {
		base.Fatalf("go: %v", err)
	}

	added := make(map[string]bool)
	for {
		ld.reset()
		if roots != nil {
			// Note: the returned roots can change on each iteration,
			// since the expansion of package patterns depends on the
			// build list we're using.
			for _, path := range roots() {
				ld.work.Add(ld.pkg(path, true))
			}
		}
		ld.work.Do(10, ld.doPkg)
		ld.buildStacks()
		numAdded := 0
		haveMod := make(map[module.Version]bool)
		for _, m := range buildList {
			haveMod[m] = true
		}
		for _, pkg := range ld.pkgs {
			if err, ok := pkg.err.(*ImportMissingError); ok && err.Module.Path != "" {
				if added[pkg.path] {
					base.Fatalf("go: %s: looping trying to add package", pkg.stackText())
				}
				added[pkg.path] = true
				numAdded++
				if !haveMod[err.Module] {
					haveMod[err.Module] = true
					buildList = append(buildList, err.Module)
				}
				continue
			}
			// Leave other errors for Import or load.Packages to report.
		}
		base.ExitIfErrors()
		if numAdded == 0 {
			break
		}

		// Recompute buildList with all our additions.
		reqs = Reqs()
		buildList, err = mvs.BuildList(Target, reqs)
		if err != nil {
			base.Fatalf("go: %v", err)
		}
	}
	base.ExitIfErrors()

	// Compute directly referenced dependency modules.
	ld.direct = make(map[string]bool)
	for _, pkg := range ld.pkgs {
		if pkg.mod == Target {
			for _, dep := range pkg.imports {
				if dep.mod.Path != "" {
					ld.direct[dep.mod.Path] = true
				}
			}
		}
	}

	// Add Go versions, computed during walk.
	ld.goVersion = make(map[string]string)
	for _, m := range buildList {
		v, _ := reqs.(*mvsReqs).versions.Load(m)
		ld.goVersion[m.Path], _ = v.(string)
	}

	// Mix in direct markings (really, lack of indirect markings)
	// from go.mod, unless we scanned the whole module
	// and can therefore be sure we know better than go.mod.
	if !ld.isALL && modFile != nil {
		for _, r := range modFile.Require {
			if !r.Indirect {
				ld.direct[r.Mod.Path] = true
			}
		}
	}
}

// pkg returns the *loadPkg for path, creating and queuing it if needed.
// If the package should be tested, its test is created but not queued
// (the test is queued after processing pkg).
// If isRoot is true, the pkg is being queued as one of the roots of the work graph.
func (ld *loader) pkg(path string, isRoot bool) *loadPkg {
	return ld.pkgCache.Do(path, func() interface{} {
		pkg := &loadPkg{
			path: path,
		}
		if ld.testRoots && isRoot || ld.testAll {
			test := &loadPkg{
				path:   path,
				testOf: pkg,
			}
			pkg.test = test
		}
		if isRoot {
			ld.roots = append(ld.roots, pkg)
		}
		ld.work.Add(pkg)
		return pkg
	}).(*loadPkg)
}

// doPkg processes a package on the work queue.
func (ld *loader) doPkg(item interface{}) {
	// TODO: what about replacements?
	pkg := item.(*loadPkg)
	var imports []string
	if pkg.testOf != nil {
		pkg.dir = pkg.testOf.dir
		pkg.mod = pkg.testOf.mod
		imports = pkg.testOf.testImports
	} else {
		if strings.Contains(pkg.path, "@") {
			// Leave for error during load.
			return
		}
		if build.IsLocalImport(pkg.path) {
			// Leave for error during load.
			// (Module mode does not allow local imports.)
			return
		}

		pkg.mod, pkg.dir, pkg.err = Import(pkg.path)
		if pkg.dir == "" {
			return
		}
		var testImports []string
		var err error
		imports, testImports, err = scanDir(pkg.dir, ld.tags)
		if err != nil {
			pkg.err = err
			return
		}
		if pkg.test != nil {
			pkg.testImports = testImports
		}
	}

	for _, path := range imports {
		pkg.imports = append(pkg.imports, ld.pkg(path, false))
	}

	// Now that pkg.dir, pkg.mod, pkg.testImports are set, we can queue pkg.test.
	// TODO: All that's left is creating new imports. Why not just do it now?
	if pkg.test != nil {
		ld.work.Add(pkg.test)
	}
}

// computePatternAll returns the list of packages matching pattern "all",
// starting with a list of the import paths for the packages in the main module.
func (ld *loader) computePatternAll(paths []string) []string {
	seen := make(map[*loadPkg]bool)
	var all []string
	var walk func(*loadPkg)
	walk = func(pkg *loadPkg) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		if pkg.testOf == nil {
			all = append(all, pkg.path)
		}
		for _, p := range pkg.imports {
			walk(p)
		}
		if p := pkg.test; p != nil {
			walk(p)
		}
	}
	for _, path := range paths {
		walk(ld.pkg(path, false))
	}
	sort.Strings(all)

	return all
}

// scanDir is like imports.ScanDir but elides known magic imports from the list,
// so that we do not go looking for packages that don't really exist.
//
// The standard magic import is "C", for cgo.
//
// The only other known magic imports are appengine and appengine/*.
// These are so old that they predate "go get" and did not use URL-like paths.
// Most code today now uses google.golang.org/appengine instead,
// but not all code has been so updated. When we mostly ignore build tags
// during "go vendor", we look into "// +build appengine" files and
// may see these legacy imports. We drop them so that the module
// search does not look for modules to try to satisfy them.
func scanDir(dir string, tags map[string]bool) (imports_, testImports []string, err error) {
	imports_, testImports, err = imports.ScanDir(dir, tags)

	filter := func(x []string) []string {
		w := 0
		for _, pkg := range x {
			if pkg != "C" && pkg != "appengine" && !strings.HasPrefix(pkg, "appengine/") &&
				pkg != "appengine_internal" && !strings.HasPrefix(pkg, "appengine_internal/") {
				x[w] = pkg
				w++
			}
		}
		return x[:w]
	}

	return filter(imports_), filter(testImports), err
}

// buildStacks computes minimal import stacks for each package,
// for use in error messages. When it completes, packages that
// are part of the original root set have pkg.stack == nil,
// and other packages have pkg.stack pointing at the next
// package up the import stack in their minimal chain.
// As a side effect, buildStacks also constructs ld.pkgs,
// the list of all packages loaded.
func (ld *loader) buildStacks() {
	if len(ld.pkgs) > 0 {
		panic("buildStacks")
	}
	for _, pkg := range ld.roots {
		pkg.stack = pkg // sentinel to avoid processing in next loop
		ld.pkgs = append(ld.pkgs, pkg)
	}
	for i := 0; i < len(ld.pkgs); i++ { // not range: appending to ld.pkgs in loop
		pkg := ld.pkgs[i]
		for _, next := range pkg.imports {
			if next.stack == nil {
				next.stack = pkg
				ld.pkgs = append(ld.pkgs, next)
			}
		}
		if next := pkg.test; next != nil && next.stack == nil {
			next.stack = pkg
			ld.pkgs = append(ld.pkgs, next)
		}
	}
	for _, pkg := range ld.roots {
		pkg.stack = nil
	}
}

// stackText builds the import stack text to use when
// reporting an error in pkg. It has the general form
//
//	import root ->
//		import other ->
//		import other2 ->
//		import pkg
//
func (pkg *loadPkg) stackText() string {
	var stack []*loadPkg
	for p := pkg.stack; p != nil; p = p.stack {
		stack = append(stack, p)
	}

	var buf bytes.Buffer
	for i := len(stack) - 1; i >= 0; i-- {
		p := stack[i]
		if p.testOf != nil {
			fmt.Fprintf(&buf, "test ->\n\t")
		} else {
			fmt.Fprintf(&buf, "import %q ->\n\t", p.path)
		}
	}
	fmt.Fprintf(&buf, "import %q", pkg.path)
	return buf.String()
}

// why returns the text to use in "go mod why" output about the given package.
// It is less ornate than the stackText but conatins the same information.
func (pkg *loadPkg) why() string {
	var buf strings.Builder
	var stack []*loadPkg
	for p := pkg; p != nil; p = p.stack {
		stack = append(stack, p)
	}

	for i := len(stack) - 1; i >= 0; i-- {
		p := stack[i]
		if p.testOf != nil {
			fmt.Fprintf(&buf, "%s.test\n", p.testOf.path)
		} else {
			fmt.Fprintf(&buf, "%s\n", p.path)
		}
	}
	return buf.String()
}

// Why returns the "go mod why" output stanza for the given package,
// without the leading # comment.
// The package graph must have been loaded already, usually by LoadALL.
// If there is no reason for the package to be in the current build,
// Why returns an empty string.
func Why(path string) string {
	pkg, ok := loaded.pkgCache.Get(path).(*loadPkg)
	if !ok {
		return ""
	}
	return pkg.why()
}

// WhyDepth returns the number of steps in the Why listing.
// If there is no reason for the package to be in the current build,
// WhyDepth returns 0.
func WhyDepth(path string) int {
	n := 0
	pkg, _ := loaded.pkgCache.Get(path).(*loadPkg)
	for p := pkg; p != nil; p = p.stack {
		n++
	}
	return n
}

// Replacement returns the replacement for mod, if any, from go.mod.
// If there is no replacement for mod, Replacement returns
// a module.Version with Path == "".
func Replacement(mod module.Version) module.Version {
	if modFile == nil {
		// Happens during testing.
		return module.Version{}
	}

	var found *modfile.Replace
	for _, r := range modFile.Replace {
		if r.Old.Path == mod.Path && (r.Old.Version == "" || r.Old.Version == mod.Version) {
			found = r // keep going
		}
	}
	if found == nil {
		return module.Version{}
	}
	return found.New
}

// mvsReqs implements mvs.Reqs for module semantic versions,
// with any exclusions or replacements applied internally.
type mvsReqs struct {
	buildList []module.Version
	cache     par.Cache
	versions  sync.Map
}

// Reqs returns the current module requirement graph.
// Future calls to SetBuildList do not affect the operation
// of the returned Reqs.
func Reqs() mvs.Reqs {
	r := &mvsReqs{
		buildList: buildList,
	}
	return r
}

func (r *mvsReqs) Required(mod module.Version) ([]module.Version, error) {
	type cached struct {
		list []module.Version
		err  error
	}

	c := r.cache.Do(mod, func() interface{} {
		list, err := r.required(mod)
		if err != nil {
			return cached{nil, err}
		}
		for i, mv := range list {
			for excluded[mv] {
				mv1, err := r.next(mv)
				if err != nil {
					return cached{nil, err}
				}
				if mv1.Version == "none" {
					return cached{nil, fmt.Errorf("%s(%s) depends on excluded %s(%s) with no newer version available", mod.Path, mod.Version, mv.Path, mv.Version)}
				}
				mv = mv1
			}
			list[i] = mv
		}

		return cached{list, nil}
	}).(cached)

	return c.list, c.err
}

var vendorOnce sync.Once

var (
	vendorList []module.Version
	vendorMap  map[string]module.Version
)

// readVendorList reads the list of vendored modules from vendor/modules.txt.
func readVendorList() {
	vendorOnce.Do(func() {
		vendorList = nil
		vendorMap = make(map[string]module.Version)
		data, _ := ioutil.ReadFile(filepath.Join(ModRoot, "vendor/modules.txt"))
		var m module.Version
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "# ") {
				f := strings.Fields(line)
				m = module.Version{}
				if len(f) == 3 && semver.IsValid(f[2]) {
					m = module.Version{Path: f[1], Version: f[2]}
					vendorList = append(vendorList, m)
				}
			} else if m.Path != "" {
				f := strings.Fields(line)
				if len(f) == 1 {
					vendorMap[f[0]] = m
				}
			}
		}
	})
}

func (r *mvsReqs) modFileToList(f *modfile.File) []module.Version {
	var list []module.Version
	for _, r := range f.Require {
		list = append(list, r.Mod)
	}
	return list
}

func (r *mvsReqs) required(mod module.Version) ([]module.Version, error) {
	if mod == Target {
		if modFile.Go != nil {
			r.versions.LoadOrStore(mod, modFile.Go.Version)
		}
		var list []module.Version
		return append(list, r.buildList[1:]...), nil
	}

	if cfg.BuildMod == "vendor" {
		// For every module other than the target,
		// return the full list of modules from modules.txt.
		readVendorList()
		return vendorList, nil
	}

	origPath := mod.Path
	if repl := Replacement(mod); repl.Path != "" {
		if repl.Version == "" {
			// TODO: need to slip the new version into the tags list etc.
			dir := repl.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(ModRoot, dir)
			}
			gomod := filepath.Join(dir, "go.mod")
			data, err := ioutil.ReadFile(gomod)
			if err != nil {
				base.Errorf("go: parsing %s: %v", base.ShortPath(gomod), err)
				return nil, ErrRequire
			}
			f, err := modfile.ParseLax(gomod, data, nil)
			if err != nil {
				base.Errorf("go: parsing %s: %v", base.ShortPath(gomod), err)
				return nil, ErrRequire
			}
			if f.Go != nil {
				r.versions.LoadOrStore(mod, f.Go.Version)
			}
			return r.modFileToList(f), nil
		}
		mod = repl
	}

	if mod.Version == "none" {
		return nil, nil
	}

	if !semver.IsValid(mod.Version) {
		// Disallow the broader queries supported by fetch.Lookup.
		base.Fatalf("go: internal error: %s@%s: unexpected invalid semantic version", mod.Path, mod.Version)
	}

	data, err := modfetch.GoMod(mod.Path, mod.Version)
	if err != nil {
		base.Errorf("go: %s@%s: %v\n", mod.Path, mod.Version, err)
		return nil, ErrRequire
	}
	f, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		base.Errorf("go: %s@%s: parsing go.mod: %v", mod.Path, mod.Version, err)
		return nil, ErrRequire
	}

	if f.Module == nil {
		base.Errorf("go: %s@%s: parsing go.mod: missing module line", mod.Path, mod.Version)
		return nil, ErrRequire
	}
	if mpath := f.Module.Mod.Path; mpath != origPath && mpath != mod.Path {
		base.Errorf("go: %s@%s: parsing go.mod: unexpected module path %q", mod.Path, mod.Version, mpath)
		return nil, ErrRequire
	}
	if f.Go != nil {
		r.versions.LoadOrStore(mod, f.Go.Version)
	}

	return r.modFileToList(f), nil
}

// ErrRequire is the sentinel error returned when Require encounters problems.
// It prints the problems directly to standard error, so that multiple errors
// can be displayed easily.
var ErrRequire = errors.New("error loading module requirements")

func (*mvsReqs) Max(v1, v2 string) string {
	if v1 != "" && semver.Compare(v1, v2) == -1 {
		return v2
	}
	return v1
}

// Upgrade is a no-op, here to implement mvs.Reqs.
// The upgrade logic for go get -u is in ../modget/get.go.
func (*mvsReqs) Upgrade(m module.Version) (module.Version, error) {
	return m, nil
}

func versions(path string) ([]string, error) {
	// Note: modfetch.Lookup and repo.Versions are cached,
	// so there's no need for us to add extra caching here.
	repo, err := modfetch.Lookup(path)
	if err != nil {
		return nil, err
	}
	return repo.Versions("")
}

// Previous returns the tagged version of m.Path immediately prior to
// m.Version, or version "none" if no prior version is tagged.
func (*mvsReqs) Previous(m module.Version) (module.Version, error) {
	list, err := versions(m.Path)
	if err != nil {
		return module.Version{}, err
	}
	i := sort.Search(len(list), func(i int) bool { return semver.Compare(list[i], m.Version) >= 0 })
	if i > 0 {
		return module.Version{Path: m.Path, Version: list[i-1]}, nil
	}
	return module.Version{Path: m.Path, Version: "none"}, nil
}

// next returns the next version of m.Path after m.Version.
// It is only used by the exclusion processing in the Required method,
// not called directly by MVS.
func (*mvsReqs) next(m module.Version) (module.Version, error) {
	list, err := versions(m.Path)
	if err != nil {
		return module.Version{}, err
	}
	i := sort.Search(len(list), func(i int) bool { return semver.Compare(list[i], m.Version) > 0 })
	if i < len(list) {
		return module.Version{Path: m.Path, Version: list[i]}, nil
	}
	return module.Version{Path: m.Path, Version: "none"}, nil
}

func fetch(mod module.Version) (dir string, isLocal bool, err error) {
	if mod == Target {
		return ModRoot, true, nil
	}
	if r := Replacement(mod); r.Path != "" {
		if r.Version == "" {
			dir = r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(ModRoot, dir)
			}
			return dir, true, nil
		}
		mod = r
	}

	dir, err = modfetch.Download(mod)
	return dir, false, err
}
