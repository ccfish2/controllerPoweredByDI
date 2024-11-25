package model

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

const (
	allhost = "*"
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

// return a lit of hosts intersecton between listener and routes
func ComputeHosts(routeHostnames []string, listenerHostname *string) []string {
	var listenerHostnameVal string
	if listenerHostname != nil && *listenerHostname != "" {
		listenerHostnameVal = *listenerHostname
	}
	if len(routeHostnames) == 0 {
		if listenerHostnameVal == "" {
			return []string{}
		} else {
			return []string{listenerHostnameVal}
		}
	}
	var hostnames []string
	for _, routehstnm := range routeHostnames {
		if listenerHostname == nil || routehstnm == listenerHostnameVal {
			hostnames = append(hostnames, routehstnm)
		}
		if strings.HasPrefix(routehstnm, allhost) {
			hostnames = append(hostnames, routehstnm)
		}
		if hostnameMatchesWildcardHostName(routehstnm, allhost) {
			hostnames = append(hostnames, routehstnm)
		}
	}
	sort.Strings(hostnames)
	return hostnames
}

func hostnameMatchesWildcardHostName(hostname, wildcardHostname string) bool {
	if !strings.HasSuffix(hostname, strings.TrimSuffix(wildcardHostname, allhost)) {
		return false
	}
	wildMatch := strings.TrimSuffix(hostname, strings.TrimPrefix(wildcardHostname, allhost))
	return len(wildMatch) > 0
}
