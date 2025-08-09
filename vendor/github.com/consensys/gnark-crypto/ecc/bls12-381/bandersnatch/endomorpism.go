package bandersnatch

import (
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

// phi endomorphism sqrt(-2) \in O(-8)
// (x,y,z)->\lambda*(x,y,z) s.t. \lamba^2 = -2 mod Order
func (p *PointProj) phi(p1 *PointProj) *PointProj {

	initOnce.Do(initCurveParams)

	var zz, yy, xy, f, g, h fr.Element
	zz.Square(&p1.Z)
	yy.Square(&p1.Y)
	xy.Mul(&p1.X, &p1.Y)
	f.Sub(&zz, &yy).Mul(&f, &curveParams.endo[1])
	zz.Mul(&zz, &curveParams.endo[0])
	g.Add(&yy, &zz).Mul(&g, &curveParams.endo[0])
	h.Sub(&yy, &zz)

	p.X.Mul(&f, &h)
	p.Y.Mul(&g, &xy)
	p.Z.Mul(&h, &xy)

	return p
}

// scalarMulGLV is the GLV scalar multiplication of a point
// p1 in projective coordinates with a scalar in big.Int
func (p *PointProj) scalarMulGLV(p1 *PointProj, scalar *big.Int) *PointProj {

	initOnce.Do(initCurveParams)

	var table [15]PointProj
	var res PointProj
	var k1, k2 fr.Element

	res.setInfinity()

	// table[b3b2b1b0-1] = b3b2*phi(p1) + b1b0*p1
	table[0].Set(p1)
	table[3].phi(p1)

	// split the scalar, modifies +-p1, phi(p1) accordingly
	k := ecc.SplitScalar(scalar, &curveParams.glvBasis)

	if k[0].Sign() == -1 {
		k[0].Neg(&k[0])
		table[0].Neg(&table[0])
	}
	if k[1].Sign() == -1 {
		k[1].Neg(&k[1])
		table[3].Neg(&table[3])
	}

	// precompute table (2 bits sliding window)
	// table[b3b2b1b0-1] = b3b2*phi(p1) + b1b0*p1 if b3b2b1b0 != 0
	table[1].Double(&table[0])
	table[2].Add(&table[1], &table[0])
	table[4].Add(&table[3], &table[0])
	table[5].Add(&table[3], &table[1])
	table[6].Add(&table[3], &table[2])
	table[7].Double(&table[3])
	table[8].Add(&table[7], &table[0])
	table[9].Add(&table[7], &table[1])
	table[10].Add(&table[7], &table[2])
	table[11].Add(&table[7], &table[3])
	table[12].Add(&table[11], &table[0])
	table[13].Add(&table[11], &table[1])
	table[14].Add(&table[11], &table[2])

	// bounds on the lattice base vectors guarantee that k1, k2 are len(r)/2 or len(r)/2+1 bits long max
	// this is because we use a probabilistic scalar decomposition that replaces a division by a right-shift
	k1 = k1.SetBigInt(&k[0]).Bits()
	k2 = k2.SetBigInt(&k[1]).Bits()

	// we don't target constant-timeness so we check first if we increase the bounds or not
	maxBit := k1.BitLen()
	if k2.BitLen() > maxBit {
		maxBit = k2.BitLen()
	}
	hiWordIndex := (maxBit - 1) / 64

	// loop starts from len(k1)/2 or len(k1)/2+1 due to the bounds
	for i := hiWordIndex; i >= 0; i-- {
		mask := uint64(3) << 62
		for j := 0; j < 32; j++ {
			res.Double(&res).Double(&res)
			b1 := (k1[i] & mask) >> (62 - 2*j)
			b2 := (k2[i] & mask) >> (62 - 2*j)
			if b1|b2 != 0 {
				scalar := (b2<<2 | b1)
				res.Add(&res, &table[scalar-1])
			}
			mask = mask >> 2
		}
	}

	p.Set(&res)
	return p
}

// phi endomorphism sqrt(-2) \in O(-8)
// (x,y,z)->\lambda*(x,y,z) s.t. \lamba^2 = -2 mod Order
func (p *PointExtended) phi(p1 *PointExtended) *PointExtended {
	initOnce.Do(initCurveParams)

	var zz, yy, xy, f, g, h fr.Element
	zz.Square(&p1.Z)
	yy.Square(&p1.Y)
	xy.Mul(&p1.X, &p1.Y)
	f.Sub(&zz, &yy).Mul(&f, &curveParams.endo[1])
	zz.Mul(&zz, &curveParams.endo[0])
	g.Add(&yy, &zz).Mul(&g, &curveParams.endo[0])
	h.Sub(&yy, &zz)

	p.X.Mul(&f, &h)
	p.Y.Mul(&g, &xy)
	p.Z.Mul(&h, &xy)
	p.T.Mul(&f, &g)

	return p
}

// scalarMulGLV is the GLV scalar multiplication of a point
// p1 in projective coordinates with a scalar in big.Int
func (p *PointExtended) scalarMulGLV(p1 *PointExtended, scalar *big.Int) *PointExtended {

	initOnce.Do(initCurveParams)

	var table [15]PointExtended
	var res PointExtended
	var k1, k2 fr.Element

	res.setInfinity()

	// table[b3b2b1b0-1] = b3b2*phi(p1) + b1b0*p1
	table[0].Set(p1)
	table[3].phi(p1)

	// split the scalar, modifies +-p1, phi(p1) accordingly
	k := ecc.SplitScalar(scalar, &curveParams.glvBasis)

	if k[0].Sign() == -1 {
		k[0].Neg(&k[0])
		table[0].Neg(&table[0])
	}
	if k[1].Sign() == -1 {
		k[1].Neg(&k[1])
		table[3].Neg(&table[3])
	}

	// precompute table (2 bits sliding window)
	// table[b3b2b1b0-1] = b3b2*phi(p1) + b1b0*p1 if b3b2b1b0 != 0
	table[1].Double(&table[0])
	table[2].Add(&table[1], &table[0])
	table[4].Add(&table[3], &table[0])
	table[5].Add(&table[3], &table[1])
	table[6].Add(&table[3], &table[2])
	table[7].Double(&table[3])
	table[8].Add(&table[7], &table[0])
	table[9].Add(&table[7], &table[1])
	table[10].Add(&table[7], &table[2])
	table[11].Add(&table[7], &table[3])
	table[12].Add(&table[11], &table[0])
	table[13].Add(&table[11], &table[1])
	table[14].Add(&table[11], &table[2])

	// bounds on the lattice base vectors guarantee that k1, k2 are len(r)/2 or len(r)/2+1 bits long max
	// this is because we use a probabilistic scalar decomposition that replaces a division by a right-shift
	k1 = k1.SetBigInt(&k[0]).Bits()
	k2 = k2.SetBigInt(&k[1]).Bits()

	// we don't target constant-timeness so we check first if we increase the bounds or not
	maxBit := k1.BitLen()
	if k2.BitLen() > maxBit {
		maxBit = k2.BitLen()
	}
	hiWordIndex := (maxBit - 1) / 64

	// loop starts from len(k1)/2 or len(k1)/2+1 due to the bounds
	for i := hiWordIndex; i >= 0; i-- {
		mask := uint64(3) << 62
		for j := 0; j < 32; j++ {
			res.Double(&res).Double(&res)
			b1 := (k1[i] & mask) >> (62 - 2*j)
			b2 := (k2[i] & mask) >> (62 - 2*j)
			if b1|b2 != 0 {
				scalar := (b2<<2 | b1)
				res.Add(&res, &table[scalar-1])
			}
			mask = mask >> 2
		}
	}

	p.Set(&res)
	return p
}
