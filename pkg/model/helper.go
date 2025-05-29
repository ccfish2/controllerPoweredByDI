package model

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
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
	if listenerHostname != nil {
		listenerHostnameVal = *listenerHostname
	}

	// No route hostnames specified: use the listener hostname if specified,
	// or else match all hostnames.
	if len(routeHostnames) == 0 {
		if len(listenerHostnameVal) > 0 {
			return []string{listenerHostnameVal}
		}

		return []string{allHosts}
	}

	var hostnames []string

	for i := range routeHostnames {
		routeHostname := routeHostnames[i]

		switch {
		// No listener hostname: use the route hostname.
		case len(listenerHostnameVal) == 0:
			hostnames = append(hostnames, routeHostname)

		// Listener hostname matches the route hostname: use it.
		case listenerHostnameVal == routeHostname:
			hostnames = append(hostnames, routeHostname)

		// Listener has a wildcard hostname: check if the route hostname matches.
		case strings.HasPrefix(listenerHostnameVal, allHosts):
			if hostnameMatchesWildcardHostname(routeHostname, listenerHostnameVal) {
				hostnames = append(hostnames, routeHostname)
			}

		// Route has a wildcard hostname: check if the listener hostname matches.
		case strings.HasPrefix(routeHostname, allHosts):
			if hostnameMatchesWildcardHostname(listenerHostnameVal, routeHostname) {
				hostnames = append(hostnames, listenerHostnameVal)
			}
		}
	}

	sort.Strings(hostnames)
	return hostnames
}

func hostnameMatchesWildcardHostname(hostname, wildcardHostname string) bool {
	if !strings.HasSuffix(hostname, strings.TrimPrefix(wildcardHostname, allHosts)) {
		return false
	}

	wildcardMatch := strings.TrimSuffix(hostname, strings.TrimPrefix(wildcardHostname, allHosts))
	return len(wildcardMatch) > 0
}

func hostnameMatchesWildcardHostName(hostname, wildcardHostname string) bool {
	if !strings.HasSuffix(hostname, strings.TrimSuffix(wildcardHostname, allhost)) {
		return false
	}
	wildMatch := strings.TrimSuffix(hostname, strings.TrimPrefix(wildcardHostname, allhost))
	return len(wildMatch) > 0
}

func AddSource(sourceList []FullyQualifiedResource, source FullyQualifiedResource) []FullyQualifiedResource {
	for _, s := range sourceList {
		if cmp.Equal(s, source) {
			return sourceList
		}
	}
	return append(sourceList, source)
}
