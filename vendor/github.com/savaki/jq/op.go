// Copyright (c) 2016 Matt Ho <matt.ho@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jq

import (
	"strings"

	"github.com/savaki/jq/scanner"
)

// Op defines a single transformation to be applied to a []byte
type Op interface {
	Apply([]byte) ([]byte, error)
}

// OpFunc provides a convenient func type wrapper on Op
type OpFunc func([]byte) ([]byte, error)

// Apply executes the transformation defined by OpFunc
func (fn OpFunc) Apply(in []byte) ([]byte, error) {
	return fn(in)
}

// Dot extract the specific key from the map provided; to extract a nested value, use the Dot Op in conjunction with the
// Chain Op
func Dot(key string) OpFunc {
	key = strings.TrimSpace(key)
	if key == "" {
		return func(in []byte) ([]byte, error) { return in, nil }
	}

	k := []byte(key)

	return func(in []byte) ([]byte, error) {
		return scanner.FindKey(in, 0, k)
	}
}

// Chain executes a series of operations in the order provided
func Chain(filters ...Op) OpFunc {
	return func(in []byte) ([]byte, error) {
		if filters == nil {
			return in, nil
		}

		var err error
		data := in
		for _, filter := range filters {
			data, err = filter.Apply(data)
			if err != nil {
				return nil, err
			}
		}

		return data, nil
	}
}

// Index extracts a specific element from the array provided
func Index(index int) OpFunc {
	return func(in []byte) ([]byte, error) {
		return scanner.FindIndex(in, 0, index)
	}
}

// Range extracts a selection of elements from the array provided, inclusive
func Range(from, to int) OpFunc {
	return func(in []byte) ([]byte, error) {
		return scanner.FindRange(in, 0, from, to)
	}
}
