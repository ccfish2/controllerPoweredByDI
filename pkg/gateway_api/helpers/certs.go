package helpers

import (
	"encoding/pem"
)

func IsValidPemFormat(b []byte) bool {
	if len(b) == 0 {
		return false
	}

	p, rest := pem.Decode(b)
	if p == nil {
		return false
	}
	if len(rest) == 0 {
		return true
	}

	// We don't check the value of `rest` because
	// Envoy will be able to parse the file as long as there
	// is at least one valid certificate.
	return true
}
