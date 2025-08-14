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

// AsArray accepts an []byte encoded json array as an input and returns the array's elements
func AsArray(in []byte, pos int) ([][]byte, error) {
	pos, err := skipSpace(in, pos)
	if err != nil {
		return nil, err
	}

	if v := in[pos]; v != '[' {
		return nil, newError(pos, v)
	}
	pos++

	// clean initial spaces
	pos, err = skipSpace(in, pos)
	if err != nil {
		return nil, err
	}

	if in[pos] == ']' {
		return [][]byte{}, nil
	}

	// 1. Count the number of elements in the array

	start := pos

	elements := make([][]byte, 0, 256)
	for {
		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		start = pos

		// data
		pos, err = Any(in, pos)
		if err != nil {
			return nil, err
		}
		elements = append(elements, in[start:pos])

		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		switch in[pos] {
		case ',':
			pos++
		case ']':
			return elements, nil
		}
	}
}
