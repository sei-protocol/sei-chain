// Package ratelimiter provides per-IP RPC rate limiting and JSON-RPC method
// extraction for endpoint protection.
//
// # MethodParser fail-closed contract
//
// MethodParser.Parse must be used on the entire request body. When MaxProbeBytes
// is positive the body must fit within that limit (see DefaultMaxProbeBytes for
// a typical cap); non-positive means no parser-imposed limit. Callers must reject on
// every returned error — including ErrProbeLimit — with no fallback decode and
// no default method bucket. Mapping any error to "admit anyway" defeats method
// extraction and allows rate-limit bypass (for example duplicate "method" keys
// where encoding/json keeps the last value).
//
// HTTP callers should map ErrProbeLimit to HTTP 413 ("request body too large")
// and all other Parse errors to HTTP 400. JSON-RPC planes should return an
// appropriate JSON-RPC error without dispatching the request.
package ratelimiter
