package ratelimiter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// DefaultMaxProbeBytes is the default MaxProbeBytes for MethodParser. The entire
// request body must fit within this limit; larger bodies yield ErrProbeLimit.
const DefaultMaxProbeBytes = 1 << 20 // 1 MiB

var (
	// ErrNoMethod is returned when a JSON-RPC request object has no "method" field.
	ErrNoMethod = errors.New("ratelimiter: JSON-RPC request has no method field")
	// ErrMethodNotString is returned when the "method" field is present but not a string.
	ErrMethodNotString = errors.New("ratelimiter: JSON-RPC method field is not a string")
	// ErrNotObject is returned when the request (or a batch element) is not a JSON object.
	ErrNotObject = errors.New("ratelimiter: JSON-RPC request is not an object")
	// ErrEmptyBatch is returned when the request is a top-level array with no elements.
	ErrEmptyBatch = errors.New("ratelimiter: JSON-RPC batch is empty")
	// ErrDuplicateMethod is returned when a request object carries more than one
	// "method" field (matched case-insensitively, as encoding/json does).
	ErrDuplicateMethod = errors.New("ratelimiter: JSON-RPC request has duplicate method field")
	// ErrTrailingData is returned when non-whitespace data follows the top-level
	// JSON-RPC value; the downstream encoding/json decode would reject such a body.
	ErrTrailingData = errors.New("ratelimiter: JSON-RPC request has trailing data")
	// ErrProbeLimit is returned when the request body exceeds MaxProbeBytes.
	ErrProbeLimit = errors.New("ratelimiter: JSON-RPC request body exceeds probe limit")
)

// IsBodyTooLarge reports whether err indicates the request body exceeded
// MaxProbeBytes. HTTP callers should reject with status 413.
func IsBodyTooLarge(err error) bool {
	return errors.Is(err, ErrProbeLimit)
}

// MethodParser extracts JSON-RPC "method" name(s) from a request body. The
// entire body must fit within MaxProbeBytes so duplicate "method" keys cannot
// hide beyond a partial read.
type MethodParser struct {
	maxProbeBytes int64
}

// NewMethodParser returns a MethodParser that accepts request bodies of at most
// maxProbeBytes bytes. Non-positive values mean unlimited (no parser-imposed cap).
func NewMethodParser(maxProbeBytes int64) *MethodParser {
	if maxProbeBytes <= 0 {
		maxProbeBytes = 0
	}
	if maxProbeBytes == math.MaxInt64 {
		// Prevent overflow when constructing the LimitedReader budget (+1).
		maxProbeBytes = math.MaxInt64 - 1
	}
	return &MethodParser{maxProbeBytes: maxProbeBytes}
}

// Parse reads a JSON-RPC request body from r and returns the method name(s) it
// carries. A single request object yields a one-element slice with batch=false;
// a top-level array yields one method per element in order with batch=true.
//
// Fail-closed contract: callers must reject on every returned error, including
// ErrProbeLimit. See the package doc for HTTP/RPC mapping guidance.
func (p *MethodParser) Parse(r io.Reader) (methods []string, batch bool, err error) {
	if p.maxProbeBytes <= 0 {
		dec := json.NewDecoder(r)
		return parseMethods(dec, nil)
	}
	// N is maxProbeBytes+1 so that lr.N reaching 0 unambiguously means the body
	// exceeded the budget.
	lr := &io.LimitedReader{R: r, N: p.maxProbeBytes + 1}
	dec := json.NewDecoder(lr)
	return parseMethods(dec, lr)
}

func parseMethods(dec *json.Decoder, lr *io.LimitedReader) (methods []string, batch bool, err error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, false, classifyErr(err, lr)
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		return nil, false, ErrNotObject
	}

	switch delim {
	case '{':
		method, err := readMethodFromObject(dec)
		if err != nil {
			return nil, false, classifyErr(err, lr)
		}
		if err := expectEOF(dec, lr); err != nil {
			return nil, false, classifyErr(err, lr)
		}
		return []string{method}, false, nil
	case '[':
		out, err := readBatchMethods(dec)
		if err != nil {
			return nil, true, classifyErr(err, lr)
		}
		if err := expectEOF(dec, lr); err != nil {
			return nil, true, classifyErr(err, lr)
		}
		return out, true, nil
	default:
		return nil, false, ErrNotObject
	}
}

// expectEOF confirms the decoder has consumed the whole body, so we can reject
// trailing non-whitespace data.
func expectEOF(dec *json.Decoder, lr *io.LimitedReader) error {
	if _, err := dec.Token(); err != nil {
		if errors.Is(err, io.EOF) && (lr == nil || lr.N > 0) {
			return nil
		}
		return err
	}
	return ErrTrailingData
}

// readBatchMethods reads the method of every element of a JSON array whose
// opening '[' has already been consumed from dec.
func readBatchMethods(dec *json.Decoder) ([]string, error) {
	var out []string
	for dec.More() {
		// Each batch element must itself be a JSON object.
		tok, err := dec.Token()
		if err != nil {
			return out, err
		}
		if delim, ok := tok.(json.Delim); !ok || delim != '{' {
			return out, ErrNotObject
		}
		method, err := readMethodFromObject(dec)
		if err != nil {
			return out, err
		}
		out = append(out, method)
	}
	// Consume and validate the closing ']' before deciding the batch is empty.
	tok, err := dec.Token()
	if err != nil {
		return out, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != ']' {
		return out, ErrNotObject
	}
	if len(out) == 0 {
		return out, ErrEmptyBatch
	}
	return out, nil
}

// readMethodFromObject reads the key/value pairs of a JSON object whose opening
// '{' has already been consumed from dec, and returns the value of its "method"
// field. The key is matched case-insensitively.
func readMethodFromObject(dec *json.Decoder) (string, error) {
	var (
		method string
		found  bool
	)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return "", err
		}
		key, ok := keyTok.(string)
		if !ok {
			// A valid JSON object always has string keys; anything else is malformed.
			return "", ErrNotObject
		}
		if strings.EqualFold(key, "method") {
			if found {
				return "", ErrDuplicateMethod
			}
			valTok, err := dec.Token()
			if err != nil {
				return "", err
			}
			s, ok := valTok.(string)
			if !ok {
				return "", ErrMethodNotString
			}
			method, found = s, true
			continue
		}
		if err := skipValue(dec); err != nil {
			return "", err
		}
	}
	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return "", err
	}
	if !found {
		return "", ErrNoMethod
	}
	return method, nil
}

// skipValue consumes exactly one complete JSON value from dec — the value whose
// first token dec is positioned at. It decodes into a json.RawMessage so the
// value's bytes are validated and drained off the stream without allocating a
// Go value per scalar the value contains.
func skipValue(dec *json.Decoder) error {
	var raw json.RawMessage
	return dec.Decode(&raw)
}

// classifyErr maps a decoder error to ErrProbeLimit when it was caused by the
// body exceeding MaxProbeBytes, and otherwise returns it wrapped.
func classifyErr(err error, lr *io.LimitedReader) error {
	if err == nil {
		return nil
	}
	if lr != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && lr.N <= 0 {
		return ErrProbeLimit
	}
	if errors.Is(err, ErrNoMethod) || errors.Is(err, ErrMethodNotString) ||
		errors.Is(err, ErrNotObject) || errors.Is(err, ErrEmptyBatch) ||
		errors.Is(err, ErrDuplicateMethod) || errors.Is(err, ErrTrailingData) {
		return err
	}
	return fmt.Errorf("ratelimiter: parse JSON-RPC method: %w", err)
}
