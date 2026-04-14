package pebbleblockdb

import (
	"bytes"
	"testing"
)

func TestBlockHeaderRoundTrip(t *testing.T) {
	hash := []byte("blockhash-0123456789abcdef")
	data := []byte("block-data-payload")
	txHashes := [][]byte{
		[]byte("tx-hash-0"),
		[]byte("tx-hash-1"),
		[]byte("tx-hash-2"),
	}

	buf := marshalBlockHeader(hash, data, txHashes)
	got, err := unmarshalBlockHeader(buf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !bytes.Equal(got.hash, hash) {
		t.Fatalf("hash: got %q, want %q", got.hash, hash)
	}
	if !bytes.Equal(got.data, data) {
		t.Fatalf("data: got %q, want %q", got.data, data)
	}
	if len(got.txHashes) != len(txHashes) {
		t.Fatalf("txHashes len: got %d, want %d", len(got.txHashes), len(txHashes))
	}
	for i := range txHashes {
		if !bytes.Equal(got.txHashes[i], txHashes[i]) {
			t.Fatalf("txHashes[%d]: got %q, want %q", i, got.txHashes[i], txHashes[i])
		}
	}
}

func TestBlockHeaderRoundTripEmpty(t *testing.T) {
	buf := marshalBlockHeader(nil, nil, nil)
	got, err := unmarshalBlockHeader(buf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.hash) != 0 {
		t.Fatalf("hash: got %q, want empty", got.hash)
	}
	if len(got.data) != 0 {
		t.Fatalf("data: got %q, want empty", got.data)
	}
	if len(got.txHashes) != 0 {
		t.Fatalf("txHashes: got %d, want 0", len(got.txHashes))
	}
}

func TestBlockHeaderRoundTripLargeTxCount(t *testing.T) {
	hash := []byte("hash-32-bytes-long-xxxxxxxxxx")
	data := []byte("some-block-data")
	const n = 1024
	txHashes := make([][]byte, n)
	for i := range txHashes {
		h := make([]byte, 32)
		h[0] = byte(i >> 8)
		h[1] = byte(i)
		txHashes[i] = h
	}

	buf := marshalBlockHeader(hash, data, txHashes)
	got, err := unmarshalBlockHeader(buf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.txHashes) != n {
		t.Fatalf("txHashes len: got %d, want %d", len(got.txHashes), n)
	}
	for i := range txHashes {
		if !bytes.Equal(got.txHashes[i], txHashes[i]) {
			t.Fatalf("txHashes[%d] mismatch", i)
		}
	}
}

func TestBlockHeaderUnmarshalOwnsMemory(t *testing.T) {
	hash := []byte("original-hash")
	data := []byte("original-data")
	txHashes := [][]byte{[]byte("tx0")}

	buf := marshalBlockHeader(hash, data, txHashes)
	got, err := unmarshalBlockHeader(buf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Corrupt the buffer; the unmarshaled struct should be unaffected.
	for i := range buf {
		buf[i] = 0xFF
	}

	if !bytes.Equal(got.hash, hash) {
		t.Fatalf("hash corrupted by buffer mutation")
	}
	if !bytes.Equal(got.data, data) {
		t.Fatalf("data corrupted by buffer mutation")
	}
	if !bytes.Equal(got.txHashes[0], txHashes[0]) {
		t.Fatalf("txHash corrupted by buffer mutation")
	}
}

func TestBlockHeaderUnmarshalTruncated(t *testing.T) {
	hash := []byte("hash")
	data := []byte("data")
	txHashes := [][]byte{[]byte("tx0"), []byte("tx1")}
	full := marshalBlockHeader(hash, data, txHashes)

	// Every prefix shorter than the full buffer should either unmarshal
	// correctly (if it happens to be a valid shorter encoding) or return
	// an error. It must never panic.
	for i := 0; i < len(full); i++ {
		truncated := full[:i]
		_, err := unmarshalBlockHeader(truncated)
		if err != nil {
			continue // expected
		}
	}
}

func TestBlockHeaderUnmarshalEmpty(t *testing.T) {
	_, err := unmarshalBlockHeader(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
	_, err = unmarshalBlockHeader([]byte{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}
