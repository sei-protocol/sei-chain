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

// FindRange finds the elements of an array between the specified indexes; inclusive
func FindRange(in []byte, pos, from, to int) ([]byte, error) {
	if to < from {
		return nil, errToLessThanFrom
	}

	pos, err := skipSpace(in, pos)
	if err != nil {
		return nil, err
	}

	if v := in[pos]; v != '[' {
		return nil, newError(pos, v)
	}
	pos++

	idx := 0
	itemStart := pos

	for {
		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		if idx == from {
			itemStart = pos
		}

		// data
		pos, err = Any(in, pos)
		if err != nil {
			return nil, err
		}

		if idx == to {
			data := in[itemStart:pos]
			result := make([]byte, 0, len(data)+2)
			result = append(result, '[')
			result = append(result, data...)
			result = append(result, ']')
			return result, nil
		}

		pos, err = skipSpace(in, pos)
		if err != nil {
			return nil, err
		}

		switch in[pos] {
		case ',':
			pos++
		case ']':
			return nil, errIndexOutOfBounds
		}

		idx++
	}
}
