package goutils

// Go's append would reuse the old slice's allocation if its capacity is sufficient, which could lead to
// nasty bugs. Consider the following example:
// ```
// import "fmt"

// func main() {
//     a := []byte("xyz")
//     a = append(a, 'p')
//     b := append(a, 'q')
//     c := append(a, 'r')
//     fmt.Println(string(a))
//     fmt.Println(string(b))
//     fmt.Println(string(c))
// }
// ```
// It would print out
// ```
// xyzp
// xyzpr
// xyzpr
// ```
// Even though most people would expect b to be xyzpq.

// Helpers in this file are meant to prevent this kind of issue by hiding away the direct `append` calls.

// Assigning the result of `append` to the same slice variable is immune from the issue shown above.
func InPlaceAppend[T ~[]I, I any](old *T, elems ...I) {
	*old = append(*old, elems...)
}

// If the result of `append` needs to be reassigned, it needs to be done in an immutable way.
func ImmutableAppend[T ~[]I, I any](old T, elems ...I) T {
	res := make([]I, len(old)+len(elems))
	copy(res, old)
	copy(res[len(old):], elems)
	return res
}
