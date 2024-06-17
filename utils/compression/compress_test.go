package compression

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type compressFunc func([]byte) ([]byte, error)
type uncompressFunc func([]byte) ([]byte, error)

func printCompressionRatio(algo string, b, bCompressed []byte) {
	originalSize := len(b)
	compressedSize := len(bCompressed)
	compressionRatio := float64(compressedSize) / float64(originalSize) * 100

	fmt.Printf("[Debug] (%s) Original size: %d bytes\n", algo, originalSize)
	fmt.Printf("[Debug] (%s), compressed size: %d bytes\n", algo, compressedSize)
	fmt.Printf("[Debug] (%s), compression ratio: %.2f%%\n", algo, compressionRatio)
}

const receiptJson = `{
	"tx_type": 0,
	"cumulative_gas_used": 0,
	"contract_address": null,
	"tx_hash_hex": "50000603341192e9af2688fcd052dfbba333d3196d40df121ca8bb92a345a2b4",
	"gas_used": 243714,
	"effective_gas_price": 1000000000,
	"block_number": 12345,
	"transaction_index": 0,
	"status": 1,
	"from": "0x70f67735d4b4d9fcfb3014da2470e2f82a8744c7",
	"to": "0x2880ab155794e7179c9ee2e38200202908c17b43",
	"vm_error": "",
	"logs": [
		{
			"address": "0x2880ab155794e7179c9ee2e38200202908c17b43",
			"topics": [
				"0xd06a6b7f4918494b3719217d1802786c1f5112a6c1d88fe2cfec00b4584f6aec",
				"0x53614f1cb0c031d4af66c04cb9c756234adad0e1cee85303795091499a4084eb"
			],
			"data": "00000000000000000000000000000000000000000000000000000000667072e700000000000000000000000000000000000000000000000000000000024c88ca000000000000000000000000000000000000000000000000000000000000d23a",
			"index": 0
		}
	],
	"logsBloom": "00000400000000000000002000000000000000000000000000000000000000000200000000000000000000000008000000000000000000000000000000000000000000000000000000400000000010000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000010001400000000000000000000000000000000000080000000000000000000000000000000000000000000000000000000000000000000000000000000100008000000000800000000000"
}`

func TestBackwardsCompatibility(t *testing.T) {
	var r types.Receipt
	err := jsonpb.Unmarshal(bytes.NewReader([]byte(receiptJson)), &r)
	require.NoError(t, err)
	receiptBytes, err := r.Marshal()
	require.NoError(t, err)

	type compressTest struct {
		Name     string
		Stored   func() []byte
		Expected []byte
		WantErr  bool
	}

	for _, test := range []compressTest{
		{
			Name: "not-compressed",
			Stored: func() []byte {
				return receiptBytes
			},
			Expected: receiptBytes,
		},
		{
			Name: "compressed",
			Stored: func() []byte {
				cb, err := CompressMessage(&r)
				require.NoError(t, err)
				return cb
			},
			Expected: receiptBytes,
		},
		{
			Name: "bad data",
			Stored: func() []byte {
				// not a receipt
				block := &types.Log{
					Address: "0x1230ab155794e7179c9ee2e38200202908c17b43",
				}
				b, err := block.Marshal()
				require.NoError(t, err)
				return b
			},
			WantErr: true,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			var result types.Receipt
			err := DecompressMessage(&result, test.Stored())
			if test.WantErr {
				require.Error(t, err)
			} else {
				// should have same bytes as expected bytes
				require.NoError(t, err)
				resultBytes, err := result.Marshal()
				require.NoError(t, err)
				require.Equal(t, test.Expected, resultBytes)
			}
		})
	}
}

func TestCompressMessage(t *testing.T) {
	var r types.Receipt
	err := jsonpb.Unmarshal(bytes.NewReader([]byte(receiptJson)), &r)
	require.NoError(t, err)

	raw, err := r.Marshal()
	require.NoError(t, err)

	compressed, err := CompressMessage(&r)
	require.NoError(t, err)

	var r2 types.Receipt
	err = DecompressMessage(&r2, compressed)
	require.NoError(t, err)

	require.Equal(t, r, r2)
	printCompressionRatio("zlib", raw, compressed)
}

func TestCompressionRatio(t *testing.T) {
	r := types.Receipt{}
	err := jsonpb.Unmarshal(bytes.NewReader([]byte(receiptJson)), &r)
	require.NoError(t, err)

	b, err := r.Marshal()
	require.NoError(t, err)

	tests := []struct {
		Name       string
		Compress   compressFunc
		Uncompress uncompressFunc
	}{
		//{
		//	Name:       "brotli",
		//	Compress:   compressBrotli,
		//	Uncompress: decompressBrotli,
		//},
		//{
		//	Name:       "bzip2",
		//	Compress:   compressBzip2,
		//	Uncompress: decompressBzip2,
		//},
		//{
		//	Name:       "lzma",
		//	Compress:   compressLzma,
		//	Uncompress: decompressLzma,
		//},
		//{
		//	Name:       "lz4",
		//	Compress:   compressLz4,
		//	Uncompress: decompressLz4,
		//},
		{
			Name:       "zlib",
			Compress:   compressZLib,
			Uncompress: decompressZLib,
		},
		//{
		//	Name:       "zstd",
		//	Compress:   compressZstd,
		//	Uncompress: decompressZstd,
		//},
		//{
		//	Name:       "snappy",
		//	Compress:   compressSnappy,
		//	Uncompress: decompressSnappy,
		//},
		//{
		//	Name:       "gzip",
		//	Compress:   compressGzip,
		//	Uncompress: decompressGzip,
		//},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			bCompressed, err := test.Compress(b)
			require.NoError(t, err)

			bUncompressed, err := test.Uncompress(bCompressed)
			require.NoError(t, err)

			require.Equal(t, string(b), string(bUncompressed), "%s: expected %s but got %s", test.Name, b, bUncompressed)
			printCompressionRatio(test.Name, b, bCompressed)
		})
	}
}

func benchmarkCompression(b *testing.B, name string, compress compressFunc, uncompress uncompressFunc) {
	r := types.Receipt{}
	err := jsonpb.Unmarshal(bytes.NewReader([]byte(receiptJson)), &r)
	require.NoError(b, err)

	data, err := r.Marshal()
	require.NoError(b, err)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		compressedData, err := compress(data)
		require.NoError(b, err)

		_, err = uncompress(compressedData)
		require.NoError(b, err)
	}
}

func BenchmarkCompression(b *testing.B) {
	benchmarks := []struct {
		name       string
		compress   compressFunc
		uncompress uncompressFunc
	}{
		//{"brotli", compressBrotli, decompressBrotli},
		//{"bzip2", compressBzip2, decompressBzip2},
		//{"lzma", compressLzma, decompressLzma},
		{"zlib", compressZLib, decompressZLib},
		//{"lz4", compressLz4, decompressLz4},
		//{"zstd", compressZstd, decompressZstd},
		//{"snappy", compressSnappy, decompressSnappy},
		//{"gzip", compressGzip, decompressGzip},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			benchmarkCompression(b, bm.name, bm.compress, bm.uncompress)
		})
	}
}
