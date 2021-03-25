package seclient

import (
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
)

func randomEmailLocalPart(rng io.Reader) (string, error) {
	b := make([]byte, ANON_EMAIL_LOCALPART_BYTES)
	_, err := rng.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func randomCapitalHexString(rng io.Reader, length int) (string, error) {
	b := make([]byte, length)
	_, err := rng.Read(b)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}
