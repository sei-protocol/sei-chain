package ed25519

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type zeroReader struct{}

func (zeroReader) Read(buf []byte) (int, error) {
	for i := range buf {
		buf[i] = 0
	}
	return len(buf), nil
}

func benchmarkKeyGeneration(b *testing.B, generateKey func(reader io.Reader) PrivKey) {
	var zero zeroReader
	for i := 0; i < b.N; i++ {
		generateKey(zero)
	}
}

func benchmarkSigning(b *testing.B, priv PrivKey) {
	message := []byte("Hello, world!")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := priv.Sign(message)
		if err != nil {
			b.FailNow()
		}
	}
}

func benchmarkVerification(b *testing.B, priv PrivKey) {
	pub := priv.PubKey()
	message := []byte("Hello, world!")
	signature, err := priv.Sign(message)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pub.VerifySignature(message, signature)
	}
}

func BenchmarkKeyGeneration(b *testing.B) {
	benchmarkKeygenWrapper := func(reader io.Reader) PrivKey {
		return genPrivKey(reader)
	}
	benchmarkKeyGeneration(b, benchmarkKeygenWrapper)
}

func BenchmarkSigning(b *testing.B) {
	priv := GenPrivKey()
	benchmarkSigning(b, priv)
}

func BenchmarkVerification(b *testing.B) {
	priv := GenPrivKey()
	benchmarkVerification(b, priv)
}

func BenchmarkVerifyBatch(b *testing.B) {
	msg := []byte("BatchVerifyTest")

	for _, sigsCount := range []int{1, 8, 64, 1024} {
		sigsCount := sigsCount
		b.Run(fmt.Sprintf("sig-count-%d", sigsCount), func(b *testing.B) {
			// Pre-generate all of the keys, and signatures, but do not
			// benchmark key-generation and signing.
			pubs := make([]PubKey, 0, sigsCount)
			sigs := make([][]byte, 0, sigsCount)
			for i := 0; i < sigsCount; i++ {
				priv := GenPrivKey()
				sig, _ := priv.Sign(msg)
				pubs = append(pubs, priv.PubKey())
				sigs = append(sigs, sig)
			}
			b.ResetTimer()

			b.ReportAllocs()
			// NOTE: dividing by n so that metrics are per-signature
			for i := 0; i < b.N/sigsCount; i++ {
				// The benchmark could just benchmark the Verify()
				// routine, but there is non-trivial overhead associated
				// with BatchVerifier.Add(), which should be included
				// in the benchmark.
				v := NewBatchVerifier()
				for i := 0; i < sigsCount; i++ {
					err := v.Add(pubs[i], msg, sigs[i])
					require.NoError(b, err)
				}

				if ok, _ := v.Verify(); !ok {
					b.Fatal("signature set failed batch verification")
				}
			}
		})
	}
}
