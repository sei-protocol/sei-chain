package utils

import (
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// ReadOnly - if a struct embeds ReadOnly,
// its private fields will be compared by TestEqual.
type ReadOnly struct{}

// isReadOnly returns true if t embeds ReadOnly.
func isReadOnly(t reflect.Type) bool {
	want := reflect.TypeOf(ReadOnly{})
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		if f := t.Field(i); f.Anonymous || f.Type == want {
			return true
		}
	}
	return false
}

func cmpComparer[T any, PT interface {
	Cmp(b *T) int
	*T
}](a PT, b PT) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

var cmpOpts = []cmp.Option{
	protocmp.Transform(),
	cmp.Exporter(isReadOnly),
	cmpopts.EquateEmpty(),
	cmp.Comparer(cmpComparer[big.Int]),
}

// TestDiff generates a human-readable diff between two objects.
func TestDiff[T any](want, got T) error {
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		return fmt.Errorf("want (-) got (+):\n%s", diff)
	}
	return nil
}

// TestEqual is a more robust replacement for reflect.DeepEqual for tests.
func TestEqual[T any](a, b T) bool {
	return cmp.Equal(a, b, cmpOpts...)
}

// TestRngSplit returns a new random number splitted from the given one.
// This is a very primitive splitting, known to result with dependent randomness.
// If that ever causes a problem, we can switch to SplitMix.
func TestRngSplit(rng *rand.Rand) *rand.Rand {
	return rand.New(rand.NewSource(rng.Int63()))
}

// TestRng returns a deterministic random number generator.
func TestRng() *rand.Rand {
	return rand.New(rand.NewSource(789345342))
}

var alphanum = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

// GenString generates a random string of length n.
func GenString(rng *rand.Rand, n int) string {
	s := make([]rune, n)
	for i := range n {
		s[i] = alphanum[rand.Intn(len(alphanum))]
	}
	return string(s)
}

// GenBytes generates a random byte slice.
func GenBytes(rng *rand.Rand, n int) []byte {
	s := make([]byte, n)
	_, _ = rng.Read(s)
	return s
}

// GenF is a function which generates T.
type GenF[T any] = func(rng *rand.Rand) T

// GenSlice generates a slice of small random length.
func GenSlice[T any](rng *rand.Rand, gen GenF[T]) []T {
	return GenSliceN(rng, 2+rng.Intn(3), gen)
}

// GenSliceN generates a slice of n elements.
func GenSliceN[T any](rng *rand.Rand, n int, gen GenF[T]) []T {
	s := make([]T, n)
	for i := range s {
		s[i] = gen(rng)
	}
	return s
}

// GenMap generates a map of small random length.
func GenMap[K comparable, V any](rng *rand.Rand, genK GenF[K], genV GenF[V]) map[K]V {
	return GenMapN(rng, 2+rng.Intn(3), genK, genV)
}

// GenMapN generates a map of n elements.
func GenMapN[K comparable, V any](rng *rand.Rand, n int, genK GenF[K], genV GenF[V]) map[K]V {
	m := make(map[K]V, n)
	for len(m) < n {
		m[genK(rng)] = genV(rng)
	}
	return m
}

// GenTimestamp generates a random timestamp.
func GenTimestamp(rng *rand.Rand) time.Time {
	return time.Unix(0, rng.Int63())
}

// GenHash generates a random Hash.
func GenHash(rng *rand.Rand) Hash {
	var h Hash
	_, _ = rng.Read(h[:])
	return h
}

// Test tests whether reencoding a value is an identity operation.
func (c ProtoConv[T, P]) Test(want T) error {
	p := c.Encode(want)
	raw, err := proto.Marshal(p)
	if err != nil {
		return fmt.Errorf("Marshal(): %w", err)
	}
	if err := proto.Unmarshal(raw, p); err != nil {
		return fmt.Errorf("Unmarshal(): %w", err)
	}
	got, err := c.Decode(p)
	if err != nil {
		return fmt.Errorf("Decode(Encode()): %w", err)
	}
	return TestDiff(want, got)
}
