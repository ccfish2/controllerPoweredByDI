package model

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

type HTTPRoute struct {
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
