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

// Object returns the position of the end of the object that begins at the specified pos
func Object(in []byte, pos int) (int, error) {
	pos, err := skipSpace(in, pos)
	if err != nil {
		return 0, err
	}

	if v := in[pos]; v != '{' {
		return 0, newError(pos, v)
	}
	pos++

	// clean initial spaces
	pos, err = skipSpace(in, pos)
	if err != nil {
		return 0, err
	}

	if in[pos] == '}' {
		return pos + 1, nil
	}

	for {
		// key
		pos, err = String(in, pos)
		if err != nil {
			return 0, err
		}

		// leading spaces
		pos, err = skipSpace(in, pos)
		if err != nil {
			return 0, err
		}

		// colon
		pos, err = expect(in, pos, ':')
		if err != nil {
			return 0, err
		}

		// data
		pos, err = Any(in, pos)
		if err != nil {
			return 0, err
		}

		pos, err = skipSpace(in, pos)
		if err != nil {
			return 0, err
		}

		switch in[pos] {
		case ',':
			pos++
		case '}':
			return pos + 1, nil
		}
	}
}
