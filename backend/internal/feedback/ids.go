package feedback

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const publicIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func GeneratePublicID() (string, error) {
	value := make([]byte, 8)
	ceiling := big.NewInt(int64(len(publicIDAlphabet)))
	for index := range value {
		random, err := rand.Int(rand.Reader, ceiling)
		if err != nil {
			return "", fmt.Errorf("generate feedback public ID: %w", err)
		}
		value[index] = publicIDAlphabet[random.Int64()]
	}
	return "fb_" + string(value), nil
}
