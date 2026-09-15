// Package passwords hashes and verifies user passwords so they are never
// stored as plain text. It uses PBKDF2-HMAC-SHA256 from the standard library
// (available since Go 1.24), so no external dependency is required.
//
// Stored hashes look like: pbkdf2-sha256$<iterations>$<salt-b64>$<hash-b64>
package passwords

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	iterations   = 600000
	saltLen      = 16
	keyLen       = 32
	maxIteration = 2000000
)

// Hash derives a salted PBKDF2-SHA256 hash of the password with a fresh
// random salt on every call.
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(derived)), nil
}

// Verify reports whether password matches the stored hash string.
func Verify(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}

	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > maxIteration {
		return false
	}

	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) < 1 {
		return false
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, iter, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(derived, expected) == 1
}
