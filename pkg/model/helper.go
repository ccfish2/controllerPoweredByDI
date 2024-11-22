package model

import (
	"crypto/sha256"
	"fmt"
)

func AddressOf[T any](v T) *T {
	return &v
}

// shorten the string to less than 63 characs
func Shorten(s string) string {
	if len(s) > 63 {
		return s[:52] + "-" + encodehash(hash(s))
	}
	return s
}

func encodehash(x string) string {
	runes := []rune(x[:10])
	for i := range runes {
		switch runes[i] {
		case '0':
			runes[i] = 'g'
		case '1':
			runes[i] = 'h'
		case '3':
			runes[i] = 'k'
		case 'a':
			runes[i] = 'm'
		case 'e':
			runes[i] = 't'
		}
	}
	return string(runes)
}
func hash(hex string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(hex)))
}

func ComputeHosts(routeHostnames []string, listenerHostname *string) []string {
	panic("rels")
}
