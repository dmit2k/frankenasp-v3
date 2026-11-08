package asp

import (
	"crypto/rand"
	"encoding/hex"
)

func nonce() string {
	b := make([]byte, 6)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}

var Nonce = nonce
