package utils

import (
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

// Thread-safe wrapper of rand.Rand.
type Rng struct{ inner *Mutex[*rand.Rand] }

func (rng Rng) Read(p []byte) (int, error) {
	for inner := range rng.inner.Lock() {
		return inner.Read(p)
	}
	panic("unreachable")
}

func (rng Rng) Int63() int64 {
	for inner := range rng.inner.Lock() {
		return inner.Int63()
	}
	panic("unreachable")
}

func (rng Rng) Int() int {
	for inner := range rng.inner.Lock() {
		return inner.Int()
	}
	panic("unreachable")
}

func (rng Rng) Intn(n int) int {
	for inner := range rng.inner.Lock() {
		return inner.Intn(n)
	}
	panic("unreachable")
}

func (rng Rng) Int63n(n int64) int64 {
	for inner := range rng.inner.Lock() {
		return inner.Int63n(n)
	}
	panic("unreachable")
}

func (rng Rng) Shuffle(n int, swap func(i, j int)) {
	for inner := range rng.inner.Lock() {
		inner.Shuffle(n, swap)
	}
}

// TestRngSplit returns a new random number splitted from the given one.
// This is a very primitive splitting, known to result with dependent randomness.
// If that ever causes a problem, we can switch to SplitMix.
func (rng Rng) Split() Rng {
	for inner := range rng.inner.Lock() {
		return TestRngFromSeed(inner.Int63())
	}
	panic("unreachable")
}

// TestRng returns a deterministic random number generator.
func TestRng() Rng {
	return TestRngFromSeed(789345342)
}

func TestRngFromSeed(seed int64) Rng {
	return Rng{Alloc(NewMutex(rand.New(rand.NewSource(seed))))}
}

func GenBool(rng Rng) bool {
	return rng.Intn(2) == 0
}

var alphanum = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

// GenString generates a random string of length n.
func GenString(rng Rng, n int) string {
	s := make([]rune, n)
	for i := range n {
		s[i] = alphanum[rand.Intn(len(alphanum))]
	}
	return string(s)
}

// GenBytes generates a random byte slice.
func GenBytes(rng Rng, n int) []byte {
	s := make([]byte, n)
	for inner := range rng.inner.Lock() {
		_, _ = inner.Read(s)
	}
	return s
}

// GenF is a function which generates T.
type GenF[T any] = func(rng Rng) T

// GenSlice generates a slice of small random length.
func GenSlice[T any](rng Rng, gen GenF[T]) []T {
	return GenSliceN(rng, 2+rng.Intn(3), gen)
}

// GenSliceN generates a slice of n elements.
func GenSliceN[T any](rng Rng, n int, gen GenF[T]) []T {
	s := make([]T, n)
	for i := range s {
		s[i] = gen(rng)
	}
	return s
}

// GenMap generates a map of small random length.
func GenMap[K comparable, V any](rng Rng, genK GenF[K], genV GenF[V]) map[K]V {
	return GenMapN(rng, 2+rng.Intn(3), genK, genV)
}

// GenMapN generates a map of n elements.
func GenMapN[K comparable, V any](rng Rng, n int, genK GenF[K], genV GenF[V]) map[K]V {
	m := make(map[K]V, n)
	for len(m) < n {
		m[genK(rng)] = genV(rng)
	}
	return m
}

// GenTimestamp generates a random timestamp.
func GenTimestamp(rng Rng) time.Time {
	return time.Unix(0, rng.Int63())
}

// GenHash generates a random Hash.
func GenHash(rng Rng) Hash {
	var h Hash
	_, _ = rng.Read(h[:])
	return h
}

// Test tests whether reencoding a value is an identity operation.
func (c *ProtoConv[T, P]) Test(want T) error {
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
