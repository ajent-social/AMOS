package codegen

import (
	"crypto/sha256"
	"encoding/hex"
)

func sha256Hex(input []byte) string {
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}
