// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modconv

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sirkon/goproxy/internal/base"
	"github.com/sirkon/goproxy/internal/modfetch"
	"github.com/sirkon/goproxy/internal/modfile"
	"github.com/sirkon/goproxy/internal/module"
	"github.com/sirkon/goproxy/internal/par"
	"github.com/sirkon/goproxy/internal/semver"
)

// ConvertLegacyConfig converts legacy config to modfile.
// The file argument is slash-delimited.
func ConvertLegacyConfig(f *modfile.File, file string, data []byte) error {
	i := strings.LastIndex(file, "/")
	j := -2
	if i >= 0 {
		j = strings.LastIndex(file[:i], "/")
	}
	convert := Converters[file[i+1:]]
	if convert == nil && j != -2 {
		convert = Converters[file[j+1:]]
	}
	if convert == nil {
		return fmt.Errorf("unknown legacy config file %s", file)
	}
	mf, err := convert(file, data)
	if err != nil {
		return fmt.Errorf("parsing %s: %v", file, err)
	}

	// Convert requirements block, which may use raw SHA1 hashes as versions,
	// to valid semver requirement list, respecting major versions.
	var work par.Work
	for _, r := range mf.Require {
		m := r.Mod
		if m.Path == "" {
			continue
		}
		work.Add(r.Mod)
	}

	var (
		mu   sync.Mutex
		need = make(map[string]string)
	)
	work.Do(10, func(item interface{}) {
		r := item.(module.Version)
		repo, info, err := modfetch.ImportRepoRev(r.Path, r.Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go: converting %s: stat %s@%s: %v\n", base.ShortPath(file), r.Path, r.Version, err)
			return
		}
		mu.Lock()
		path := repo.ModulePath()
		// Don't use semver.Max here; need to preserve +incompatible suffix.
		if v, ok := need[path]; !ok || semver.Compare(v, info.Version) < 0 {
			need[path] = info.Version
		}
		mu.Unlock()
	})

	var paths []string
	for path := range need {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		f.AddNewRequire(path, need[path], false)
	}

	for _, r := range mf.Replace {
		err := f.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version)
		if err != nil {
			return fmt.Errorf("add replace: %v", err)
		}
	}
	f.Cleanup()
	return nil
}
