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

// Number returns the end position of the number that begins at the specified pos
func Number(in []byte, pos int) (int, error) {
	pos, err := skipSpace(in, pos)
	if err != nil {
		return 0, err
	}

	max := len(in)
	for {
		v := in[pos]
		switch v {
		case '-', '+', '.', 'e', 'E', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0':
			pos++
		default:
			return pos, nil
		}

		if pos >= max {
			return pos, nil
		}
	}

	return pos, nil
}
