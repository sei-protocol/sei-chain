// Copyright (c) 2016-2019 isis agora lovecruft. All rights reserved.
// Copyright (c) 2016-2019 Henry de Valence. All rights reserved.
// Copyright (c) 2020-2021 Oasis Labs Inc. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
// 1. Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//
// 2. Redistributions in binary form must reproduce the above copyright
// notice, this list of conditions and the following disclaimer in the
// documentation and/or other materials provided with the distribution.
//
// 3. Neither the name of the copyright holder nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
// IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
// PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED
// TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
// PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
// LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
// NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
// SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Package field implements field arithmetic modulo p = 2^255 - 19.
package field

import "github.com/oasisprotocol/curve25519-voi/internal/subtle"

// FieldElementSize is the size of a field element in bytes.
const FieldElementSize = 32

var (
	// One is the field element one.
	One = func() FieldElement {
		var one FieldElement
		one.One()
		return one
	}()

	// MinusOne is the field element -1.
	MinusOne = func() FieldElement {
		var minusOne FieldElement
		minusOne.MinusOne()
		return minusOne
	}()
)

// Set sets fe to t, and returns fe.
func (fe *FieldElement) Set(t *FieldElement) *FieldElement {
	*fe = *t
	return fe
}

// Zero sets fe to zero, and returns fe.
func (fe *FieldElement) Zero() *FieldElement {
	for i := range fe.inner {
		fe.inner[i] = 0
	}
	return fe
}

// Equal returns 1 iff the field elements are equal, 0
// otherwise.  This function will execute in constant-time.
func (fe *FieldElement) Equal(other *FieldElement) int {
	var selfBytes, otherBytes [FieldElementSize]byte
	_ = fe.ToBytes(selfBytes[:])
	_ = other.ToBytes(otherBytes[:])

	return subtle.ConstantTimeCompareBytes(selfBytes[:], otherBytes[:])
}

// IsNegative returns 1 iff the field element is negative, 0 otherwise.
func (fe *FieldElement) IsNegative() int {
	var selfBytes [FieldElementSize]byte
	_ = fe.ToBytes(selfBytes[:])

	return int(selfBytes[0] & 1)
}

// ConditionalNegate negates the field element iff choice == 1, leaves
// it unchanged otherwise.
func (fe *FieldElement) ConditionalNegate(choice int) {
	var feNeg FieldElement
	fe.ConditionalAssign(feNeg.Neg(fe), choice)
}

// IsZero returns 1 iff the field element is zero, 0 otherwise.
func (fe *FieldElement) IsZero() int {
	var selfBytes, zeroBytes [FieldElementSize]byte
	_ = fe.ToBytes(selfBytes[:])

	return subtle.ConstantTimeCompareBytes(selfBytes[:], zeroBytes[:])
}

// Invert sets fe to the multiplicative inverse of t, and returns fe.
//
// The inverse is computed as self^(p-2), since x^(p-2)x = x^(p-1) = 1 (mod p).
//
// On input zero, the field element is set to zero.
func (fe *FieldElement) Invert(t *FieldElement) *FieldElement {
	// The bits of p-2 = 2^255 -19 -2 are 11010111111...11.
	//
	//                       nonzero bits of exponent
	tmp, t3 := t.pow22501()  // t19: 249..0 ; t3: 3,1,0
	tmp.Pow2k(&tmp, 5)       // 254..5
	return fe.Mul(&tmp, &t3) // 254..5,3,1,0
}

// SqrtRatioI sets the fe to either `sqrt(u/v)` or `sqrt(i*u/v)` in constant
// time, and returns fe.  This function always selects the nonnegative square
// root.
func (fe *FieldElement) SqrtRatioI(u, v *FieldElement) (*FieldElement, int) {
	var v3 FieldElement
	v3.Square(v)
	v3.Mul(&v3, v)

	var v7 FieldElement
	v7.Square(&v3)
	v7.Mul(&v7, v)

	var r FieldElement
	r.Mul(u, &v7)
	r.pow_p58()
	r.Mul(&r, &v3)
	r.Mul(&r, u)

	var check FieldElement
	check.Square(&r)
	check.Mul(&check, v)

	var neg_u, neg_u_i FieldElement
	neg_u.Neg(u)
	neg_u_i.Mul(&neg_u, &SQRT_M1)

	correct_sign_sqrt := check.Equal(u)
	flipped_sign_sqrt := check.Equal(&neg_u)
	flipped_sign_sqrt_i := check.Equal(&neg_u_i)

	var r_prime FieldElement
	r_prime.Mul(&r, &SQRT_M1)
	r.ConditionalAssign(&r_prime, flipped_sign_sqrt|flipped_sign_sqrt_i)

	// Chose the nonnegative square root.
	r_is_negative := r.IsNegative()
	r.ConditionalNegate(r_is_negative)

	fe.Set(&r)

	return fe, correct_sign_sqrt | flipped_sign_sqrt
}

// InvSqrt attempts to set `fe = sqrt(1/self)` in constant time, and return fe.
// This function always selects the nonnegative square root.
func (fe *FieldElement) InvSqrt() (*FieldElement, int) {
	return fe.SqrtRatioI(&One, fe)
}

// pow22501 returns (self^(2^250-1), self^11), used as a helper function
// within Invert() and pow_p58().
func (fe *FieldElement) pow22501() (FieldElement, FieldElement) {
	// TODO/perf: Reducing the number of temporary variables used
	// is likely a performance gain (See the Montgomery multiplication
	// helper for an example of this), though it's relatively minor.
	var t0, t1, t2, t3, t4, t5, t6, t7, t8, t9, t10, t11, t12, t13, t14, t15, t16, t17, t18, t19 FieldElement

	t0.Square(fe)

	t1.Square(&t0)
	t1.Square(&t1)

	t2.Mul(fe, &t1)

	t3.Mul(&t0, &t2)

	t4.Square(&t3)

	t5.Mul(&t2, &t4)

	t6.Pow2k(&t5, 5)

	t7.Mul(&t6, &t5)

	t8.Pow2k(&t7, 10)

	t9.Mul(&t8, &t7)

	t10.Pow2k(&t9, 20)

	t11.Mul(&t10, &t9)

	t12.Pow2k(&t11, 10)

	t13.Mul(&t12, &t7)

	t14.Pow2k(&t13, 50)

	t15.Mul(&t14, &t13)

	t16.Pow2k(&t15, 100)

	t17.Mul(&t16, &t15)

	t18.Pow2k(&t17, 50)

	t19.Mul(&t18, &t13)

	return t19, t3
}

// pow_p58 raises the field element to the power (p-5)/8 = 2^252 - 3,
// and returns fe.
func (fe *FieldElement) pow_p58() {
	// The bits of (p-5)/8 are 101111.....11.
	//
	//                      nonzero bits of exponent
	tmp, _ := fe.pow22501() // 249..0
	tmp.Pow2k(&tmp, 2)      // 251..2
	fe.Mul(fe, &tmp)        // 251..2,0
}

// BatchInvert computes the inverses of slice of `FieldElements`s
// in a batch, and replaces each element by its inverse.
//
// WARNING: The input field elements MUST be nonzero.  If you cannot prove
// that this is the case you MUST not use this function.
func BatchInvert(inputs []*FieldElement) {
	// Montgomery’s Trick and Fast Implementation of Masked AES
	// Genelle, Prouff and Quisquater
	// Section 3.2
	n := len(inputs)
	scratch := make([]FieldElement, n)
	for i := range scratch {
		scratch[i].One()
	}

	// Keep an accumulator of all of the previous products.
	acc := One

	// Pass through the input vector, recording the previous
	// products in the scratch space.
	for i, input := range inputs {
		scratch[i] = acc
		acc.Mul(&acc, input)
	}

	// Compute the inverse of all products.
	acc.Invert(&acc)

	// Pass through the vector backwards to compute the inverses
	// in place.
	for i := n - 1; i >= 0; i-- {
		input, scratch := inputs[i], scratch[i]
		var tmp FieldElement
		tmp.Mul(&acc, input)
		inputs[i].Mul(&acc, &scratch)
		acc = tmp
	}
}
