package seclient

import (
	crand "crypto/rand"
	"math/big"
)

type secureRandomSource struct{}

var RandomSource secureRandomSource

var int63Limit = big.NewInt(0).Lsh(big.NewInt(1), 63)

func (_ secureRandomSource) Seed(_ int64) {
}

func (_ secureRandomSource) Int63() int64 {
	randNum, err := crand.Int(crand.Reader, int63Limit)
	if err != nil {
		panic(err)
	}
	return randNum.Int64()
}
