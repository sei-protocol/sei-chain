// Package jp offers a highly performant json selector in the style of the jq command line
//
// Usage of this package involves the concept of an Op.  An Op is a transformation that converts a []byte into a []byte.
// To get started, use the Parse function to obtain an Op.
//
//     op, err := jq.Parse(".key")
//
// This will create an Op that will accept a JSON object in []byte format and return the value associated with "key."
// For example:
//
//     in := []byte(`{"key":"value"}`)
//     data, _ := op.Apply(in))
//     fmt.Println(string(data))
//
// Will print the string "value".  The goal is to support all the select operations supported by jq's command line
// namesake.
//
package jq
