package seclient

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

func capitalHexSHA1(input string) string {
	h := sha1.Sum([]byte(input))
	return strings.ToUpper(hex.EncodeToString(h[:]))
}
