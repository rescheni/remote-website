package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

var pendingRequests sync.Map

func newRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
