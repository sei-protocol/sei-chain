package ed25519

import (
	"fmt"
	"testing"
)

func BenchmarkSigning(b *testing.B) {
	priv := TestSecretKey([]byte("test"))
	message := []byte("Hello, world!")
	for b.Loop() {
		priv.Sign(message)
	}
}

func BenchmarkVerification(b *testing.B) {
	priv := TestSecretKey([]byte("test"))
	pub := priv.Public()
	message := []byte("Hello, world!")
	sig := priv.Sign(message)
	for b.Loop() {
		pub.Verify(message, sig)
	}
}

func BenchmarkVerifyBatch(b *testing.B) {
	msg := []byte("BatchVerifyTest")
	for _, sigsCount := range []int{1, 8, 64, 1024} {
		b.Run(fmt.Sprintf("sig-count-%d", sigsCount), func(b *testing.B) {
			// Pre-generate all of the keys, and signatures, but do not
			// benchmark key-generation and signing.
			pubs := make([]PublicKey, 0, sigsCount)
			sigs := make([]Signature, 0, sigsCount)
			for i := range sigsCount {
				priv := TestSecretKey(fmt.Appendf(nil, "test-%v", i))
				pubs = append(pubs, priv.Public())
				sigs = append(sigs, priv.Sign(msg))
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
				for i := range sigsCount {
					v.Add(pubs[i], msg, sigs[i])
				}
				if err := v.Verify(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
