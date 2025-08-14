// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modconv

import (
	"strings"

	"github.com/sirkon/goproxy/internal/modfile"
	"github.com/sirkon/goproxy/internal/module"
)

func ParseDependenciesTSV(file string, data []byte) (*modfile.File, error) {
	mf := new(modfile.File)
	for lineno, line := range strings.Split(string(data), "\n") {
		lineno++
		f := strings.Split(line, "\t")
		if len(f) >= 3 {
			mf.Require = append(mf.Require, &modfile.Require{Mod: module.Version{Path: f[0], Version: f[2]}})
		}
	}
	return mf, nil
}
