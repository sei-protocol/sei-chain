# jq

[![GoDoc](https://godoc.org/github.com/savaki/jq?status.svg)](https://godoc.org/github.com/savaki/jq)
[![Build Status](https://snap-ci.com/savaki/jq/branch/master/build_image)](https://snap-ci.com/savaki/jq/branch/master)

A high performance Golang implementation of the incredibly useful jq command line tool.

Rather than marshalling json elements into go instances, jq opts to manipulate the json elements as raw []byte.  This
is especially useful for apps that need to handle dynamic json data.

Using jq consists of creation an ```Op``` and then calling ```Apply``` on the ```Op``` to transform one []byte into the 
desired []byte.  Ops may be chained together to form a transformation chain similar to how the command line jq works.   

## Installation

```
go get github.com/savaki/jq
```

## Example

```go
package main

import (
	"fmt"

	"github.com/savaki/jq"
)

func main() {
	op, _ := jq.Parse(".hello")           // create an Op
	data := []byte(`{"hello":"world"}`)   // sample input
	value, _ := op.Apply(data)            // value == '"world"'
	fmt.Println(string(value))
}
```

## Syntax

The initial goal is to support all the selectors the original jq command line supports.

| syntax | meaning|
| :--- | :--- |
| . |  unchanged input |
| .foo |  value at key |
| .foo.bar |  value at nested key |
| .[0] | value at specified element of array | 
| .[0:1] | array of specified elements of array, inclusive |
| .foo.[0] | nested value |

## Examples

### Data
```json
{
  "string": "a",
  "number": 1.23,
  "simple": ["a", "b", "c"],
  "mixed": [
    "a",
    1,
    {"hello":"world"}
  ],
  "object": {
    "first": "joe",
    "array": [1,2,3]
  }
}
```

| syntax | value |
| :--- | :--- |
| .string | "a" |
| .number| 1.23 |
| .simple | ["a", "b", "c"] |
| .simple.[0] | "a" |
| .simple.[0:1] | ["a","b"] |
| .mixed.[1] | 1
| .object.first | "joe" |
| .object.array.[2] | 3 |

## Performance

```
BenchmarkAny-8         	20000000	        80.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkArray-8       	20000000	       108 ns/op	       0 B/op	       0 allocs/op
BenchmarkFindIndex-8   	10000000	       125 ns/op	       0 B/op	       0 allocs/op
BenchmarkFindKey-8     	10000000	       125 ns/op	       0 B/op	       0 allocs/op
BenchmarkFindRange-8   	10000000	       186 ns/op	      16 B/op	       1 allocs/op
BenchmarkNumber-8      	50000000	        28.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkObject-8      	20000000	        98.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkString-8      	30000000	        40.4 ns/op	       0 B/op	       0 allocs/op
```

