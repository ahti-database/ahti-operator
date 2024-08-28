package utils

import (
	"crypto/ed25519"
	"crypto/rand"

	"github.com/golang-jwt/jwt/v5"
)

// function to generate private/public key used for signing jwt tokens
func GenerateAsymmetricKeys() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	return publicKey, privateKey, err
}

func GenerateJWT(key []byte) (string, error) {
	t := jwt.New(jwt.SigningMethodEdDSA)
	jwt, err := t.SignedString(key)
	return jwt, err
}
