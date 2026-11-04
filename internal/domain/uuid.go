package domain

import (
	"crypto/rand"
	"fmt"
)

// NewUUID returns a random RFC 9562 version 4 UUID without a database extension.
func NewUUID() string {
	var b [16]byte
	// Go's crypto/rand.Read fills the buffer or terminates on entropy failure.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
