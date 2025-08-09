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

var (
	n = []byte("null")
)

// Null verifies the contents of bytes provided is a null starting as pos
func Null(in []byte, pos int) (int, error) {
	switch in[pos] {
	case 'n':
		return expect(in, pos, n...)
		return pos + 4, nil
	default:
		return 0, errUnexpectedValue
	}
}
