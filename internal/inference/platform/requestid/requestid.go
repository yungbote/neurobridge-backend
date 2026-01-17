package requestid

import (
	"crypto/rand"
	"encoding/hex"
)

func New() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
