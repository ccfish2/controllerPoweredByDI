package model

import (
	"strconv"
	"time"
)

type Model struct {
	HTTP []HTTPListener `json:"http,omitempty"`
	TLS  []TLSListener  `json:"tls,omitempty"`
}

func (m *Model) GetListerners() []Listener {
	var listeners []Listener
	for _, lis := range m.HTTP {
		listeners = append(listeners, &lis)
	}
	for _, lis := range m.TLS {
		listeners = append(listeners, &lis)
	}
	return listeners
}

type Listener interface {
	GetSources() []FullyQualifiedResource
	GetPort() uint32
	GetAnnotations() map[string]string
	GetLabels() map[string]string
}

type FullyQualifiedResource struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind,omitempty"`
	UID       string `json:"uuid,omitempty"`
}

type TLSSecret struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type Service struct {
	Type             string  `json:"serviceType,omitempty"`
	InsecureNodePort *uint32 `json:"insecureNodePort,omitempty"`
	SecureNodePort   *uint32 `json:"secureNodePort,omitempty"`
}

type StringMatch struct {
	Prefix string
	Exact  string
	Regex  string
}
type KeyValueMatch struct {
	Key   string
	Match StringMatch
}

type Header struct {
	Name  string
	Value string
}

type HttpHeaderFilter struct {
	HeadersToAdd    []Header
	HeadersToSet    []Header
	HeadersToRemove []string
}

type BackendHttpFilter struct {
	Name                   string            `json:"name,omitempty"`
	RequestHeaderFilter    *HttpHeaderFilter `json:"request_header_filter,omitempty"`
	ResponseHeaderModifier *HttpHeaderFilter `json:"response_header_modifier,omitempty"`
}

type DirectResponse struct {
	StatusCode int
	Body       string
}

type HTTPRoute struct {
	Name               string              `json:"name,omitempty"`
	Hostnames          string              `json:"hostnames,omitempty"`
	PatchMatch         StringMatch         `json:"patch_match,omitempty"`
	HeadersMatch       []KeyValueMatch     `json:"headers_match,omitempty"`
	QueryParamsMatch   []KeyValueMatch     `json:"query_params_match,omitempty"`
	Method             *string             `json:"method,omitempty"`
	Backends           []Backend           `json:"backends,omitempty"`
	BackendHttpFilters []BackendHttpFilter `json:"backend_http_filters,omitempty"`
	DirectResponse     *DirectResponse     `json:"direct_response,omitempty"`

	RequestHeadFile     *HttpHeaderFilter `json:"request_head_file,omitempty"`
	ResponseHeaderMatch *HttpHeaderFilter `json:"response_header_match,omitempty"`

	RequestRedirected *HTTPRequestRedirectFilter `json:"request_redirected,omitempty"`

	Rewrite *HTTPURLRewriteFilter `json:"rewrite,omitempty"`

	RequestMirrors []*HttpRequestMirror `json:"request_mirrors,omitempty"`
	IsGRPC         bool                 `json:"is_grpc,omitempty"`
	Timeout        Timeout              `json:"timeout,omitempty"`
}

type Timeout struct {
	Request time.Duration `json:"request,omitempty"`
	Backend time.Duration `json:"backend,omitempty"`
}

type HttpRequestMirror struct {
	Backend *Backend `json:"backend,omitempty"`
}

type HTTPRequestRedirectFilter struct {
	Scheme     *string      `json:"scheme,omitempty"`
	Hostname   *string      `json:"hostname,omitempty"`
	Path       *StringMatch `json:"path,omitempty"`
	Port       *int32       `json:"port,omitempty"`
	StatusCode *int         `json:"status_code,omitempty"`
}

type HTTPURLRewriteFilter struct {
	Hostname *string      `json:"hostname,omitempty"`
	Path     *StringMatch `json:"path,omitempty"`
}

type Infrastructure struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type HTTPListener struct {
	Name           string                   `json:"name,omitempty"`
	Sources        []FullyQualifiedResource `json:"sources,omitempty"`
	Address        string                   `json:"address,omitempty"`
	Port           uint32                   `json:"port,omitempty"`
	Hostname       string                   `json:"hostname,omitempty"`
	TLS            []TLSSecret              `json:"tls,omitempty"`
	Routes         []HTTPRoute              `json:"routes,omitempty"`
	Service        *Service                 `json:"service,omitempty"`
	Infrastructure *Infrastructure          `json:"infrastructure,omitempty"`
}

// GetAnnotation implements Listener.
func (h *HTTPListener) GetAnnotations() map[string]string {
	if h.Infrastructure != nil {
		return h.Infrastructure.Annotations
	}
	return nil
}

// GetLabels implements Listener.
func (h *HTTPListener) GetLabels() map[string]string {
	if h.Infrastructure != nil {
		return h.Infrastructure.Labels
	}
	return nil
}

// GetPort implements Listener.
func (h *HTTPListener) GetPort() uint32 {
	return h.Port
}

// GetSources implements Listener.
func (h *HTTPListener) GetSources() []FullyQualifiedResource {
	return h.Sources
}

type TLSListener struct {
	Name           string                   `json:"name,omitempty"`
	Sources        []FullyQualifiedResource `json:"sources,omitempty"`
	Address        string                   `json:"address,omitempty"`
	Port           uint32                   `json:"port,omitempty"`
	Hostname       string                   `json:"hostname,omitempty"`
	Routes         []TLSRoute               `json:"routes,omitempty"`
	Service        *Service                 `json:"service,omitempty"`
	Infrastructure *Infrastructure          `json:"infrastructure,omitempty"`
}

// GetAnnotation implements Listener.
func (t *TLSListener) GetAnnotations() map[string]string {
	if t.Infrastructure != nil {
		return t.Infrastructure.Annotations
	}
	return nil
}

// GetLabels implements Listener.
func (t *TLSListener) GetLabels() map[string]string {
	if t.Infrastructure != nil {
		return t.Infrastructure.Labels
	}
	return nil
}

// GetPort implements Listener.
func (t *TLSListener) GetPort() uint32 {
	return t.Port
}

// GetSources implements Listener.
func (t *TLSListener) GetSources() []FullyQualifiedResource {
	return t.Sources
}

type BackendPort struct {
	Port uint32 `json:"port,omitempty"`
	Name string `json:"name,omitempty"`
}

type Backend struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	Port   *BackendPort `json:"port,omitempty"`
	Weight *int32       `json:"weight,omitempty"`
}

type TLSRoute struct {
	Name      string    `json:"name,omitempty"`
	Hostnames []string  `json:"hostnames,omitempty"`
	Backends  []Backend `json:"backends,omitempty"`
}

func (b *BackendPort) GetPort() string {
	if b.Port != 0 {
		return strconv.Itoa(int(b.Port))
	}
	return b.Name
}
