package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		fmt.Fprintf(os.Stderr, "buildversion: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("act-gui-dev-%s\n", hex.EncodeToString(nonce[:]))
}
