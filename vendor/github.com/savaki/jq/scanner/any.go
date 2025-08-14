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

// Any returns the position of the end of the current element that begins at pos; handles any valid json element
func Any(in []byte, pos int) (int, error) {
	pos, err := skipSpace(in, pos)
	if err != nil {
		return 0, err
	}

	switch in[pos] {
	case '"':
		return String(in, pos)
	case '{':
		return Object(in, pos)
	case '.', '-', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0':
		return Number(in, pos)
	case '[':
		return Array(in, pos)
	case 't', 'f':
		return Boolean(in, pos)
	case 'n':
		return Null(in, pos)
	default:
		max := len(in) - pos
		if max > 20 {
			max = 20
		}

		return 0, opErr{
			pos:     pos,
			msg:     "invalid object",
			content: string(in[pos : pos+max]),
		}
	}
}
