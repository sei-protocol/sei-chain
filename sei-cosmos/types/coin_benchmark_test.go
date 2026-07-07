package types

import (
	"fmt"
	"testing"
)

func coinName(suffix int) string {
	return fmt.Sprintf("coinz%d", suffix)
}

func BenchmarkCoinsAdditionIntersect(b *testing.B) {
	b.ReportAllocs()
	benchmarkingFunc := func(numCoinsA int, numCoinsB int) func(b *testing.B) {
		return func(b *testing.B) {
			b.ReportAllocs()
			coinsA := Coins(make([]Coin, numCoinsA))
			coinsB := Coins(make([]Coin, numCoinsB))

			for i := 0; i < numCoinsA; i++ {
				coinsA[i] = NewCoin(coinName(i), NewInt(int64(i)))
			}
			for i := 0; i < numCoinsB; i++ {
				coinsB[i] = NewCoin(coinName(i), NewInt(int64(i)))
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				coinsA.Add(coinsB...)
			}
		}
	}

	benchmarkSizes := [][]int{{1, 1}, {5, 5}, {5, 20}, {1, 1000}, {2, 1000}}
	for i := 0; i < len(benchmarkSizes); i++ {
		sizeA := benchmarkSizes[i][0]
		sizeB := benchmarkSizes[i][1]
		b.Run(fmt.Sprintf("sizes: A_%d, B_%d", sizeA, sizeB), benchmarkingFunc(sizeA, sizeB))
	}
}

func BenchmarkCoinsAdditionNoIntersect(b *testing.B) {
	b.ReportAllocs()
	benchmarkingFunc := func(numCoinsA int, numCoinsB int) func(b *testing.B) {
		return func(b *testing.B) {
			b.ReportAllocs()
			coinsA := Coins(make([]Coin, numCoinsA))
			coinsB := Coins(make([]Coin, numCoinsB))

			for i := 0; i < numCoinsA; i++ {
				coinsA[i] = NewCoin(coinName(numCoinsB+i), NewInt(int64(i)))
			}
			for i := 0; i < numCoinsB; i++ {
				coinsB[i] = NewCoin(coinName(i), NewInt(int64(i)))
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				coinsA.Add(coinsB...)
			}
		}
	}

	benchmarkSizes := [][]int{{1, 1}, {5, 5}, {5, 20}, {1, 1000}, {2, 1000}, {1000, 2}}
	for i := 0; i < len(benchmarkSizes); i++ {
		sizeA := benchmarkSizes[i][0]
		sizeB := benchmarkSizes[i][1]
		b.Run(fmt.Sprintf("sizes: A_%d, B_%d", sizeA, sizeB), benchmarkingFunc(sizeA, sizeB))
	}
}

// sortedCoins builds a valid Coins list of n coins with denoms in ascending
// order (zero-padded so lexical order is numeric order), satisfying the
// sorted/no-duplicate/positive-amount invariant DenomsSubsetOf relies on.
func sortedCoins(n int) Coins {
	coins := make(Coins, n)
	for i := 0; i < n; i++ {
		coins[i] = NewCoin(fmt.Sprintf("coin%08d", i), OneInt())
	}
	return coins
}

// BenchmarkCoinsDenomsSubsetOf guards against a regression to the quadratic
// DenomsSubsetOf implementation (Immunefi 79950). With the two-pointer merge the
// worst case (receiver == B, full walk) is linear in the list length; the old
// AmountOf-per-element version was O(n*m) and blew up at large sizes.
func BenchmarkCoinsDenomsSubsetOf(b *testing.B) {
	benchmarkingFunc := func(size int) func(b *testing.B) {
		return func(b *testing.B) {
			b.ReportAllocs()
			// receiver == B forces the full walk (true subset), the worst case.
			coins := sortedCoins(size)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if !coins.DenomsSubsetOf(coins) {
					b.Fatal("expected subset")
				}
			}
		}
	}

	for _, size := range []int{10, 100, 1000, 10000, 56500} {
		b.Run(fmt.Sprintf("size_%d", size), benchmarkingFunc(size))
	}
}
