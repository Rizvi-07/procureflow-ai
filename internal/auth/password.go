// Package auth contains local accounts, browser sessions, and internal identities.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const hashPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"

func RandomSecret() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic("cryptographic random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 128 {
		return "", fmt.Errorf("password must contain 12-128 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return hashPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func parseHash(encoded string) ([]byte, []byte, error) {
	if !strings.HasPrefix(encoded, hashPrefix) {
		return nil, nil, fmt.Errorf("unsupported password hash")
	}
	parts := strings.Split(strings.TrimPrefix(encoded, hashPrefix), "$")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	key, keyErr := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || keyErr != nil || len(salt) != 16 || len(key) != 32 {
		return nil, nil, fmt.Errorf("invalid password hash")
	}
	return salt, key, nil
}

func VerifyPassword(encoded, password string) bool {
	if len(password) > 128 {
		return false
	}
	salt, expected, err := parseHash(encoded)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
