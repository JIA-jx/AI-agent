package randoms

import (
	cr "crypto/rand"
	"fmt"
	v2 "math/rand/v2"
)

func GenCode() (string, error) {
	var seed [32]byte
	if _, err := cr.Read(seed[:]); err != nil {
		return "", err
	}

	cha8 := v2.NewChaCha8(seed)
	r := v2.New(cha8)
	num := r.IntN(1000000)

	return fmt.Sprintf("%06d", num), nil
}
