// SPDX-License-Identifier: AGPL-3.0-only
package xray

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

func NewRealityKeys() (string, string, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return encodeRealityKey(priv.Bytes()), encodeRealityKey(priv.PublicKey().Bytes()), nil
}

func PublicKeyFromPrivate(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return "", err
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return encodeRealityKey(priv.PublicKey().Bytes()), nil
}

func NewRealityShortID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", []byte("zxy00001"))
	}
	return hex.EncodeToString(b)
}

func encodeRealityKey(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}
