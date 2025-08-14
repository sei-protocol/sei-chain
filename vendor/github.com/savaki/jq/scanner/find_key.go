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

package scanner

import "bytes"

// FindKey accepts a JSON object and returns the value associated with the key specified
func FindKey(in []byte, pos int, k []byte) ([]byte, error) {
	pos, err := skipSpace(in, pos)
	if err != nil {
		return nil, err
	}

	if v := in[pos]; v != '{' {
		return nil, newError(pos, v)
	}
	pos++

	for {
		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		keyStart := pos
		// key
		pos, err = String(in, pos)
		if err != nil {
			return nil, err
		}
		key := in[keyStart+1 : pos-1]
		match := bytes.Equal(k, key)

		// leading spaces
		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		// colon
		pos, err = expect(in, pos, ':')
		if err != nil {
			return nil, err
		}

		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		valueStart := pos
		// data
		pos, err = Any(in, pos)
		if err != nil {
			return nil, err
		}

		if match {
			return in[valueStart:pos], nil
		}

		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		switch in[pos] {
		case ',':
			pos++
		case '}':
			return nil, errKeyNotFound
		}
	}
}
