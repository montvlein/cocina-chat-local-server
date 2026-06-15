package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GenerateInviteToken() (full, prefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	full = "inv_" + hex.EncodeToString(b)
	if len(full) > 12 {
		prefix = full[:12] + "..."
	}
	return full, prefix, nil
}
