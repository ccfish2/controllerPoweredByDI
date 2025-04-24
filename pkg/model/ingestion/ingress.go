package ingestion

import (
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	networkingv1 "k8s.io/api/networking/v1"
)

// translate ingress into TLSListener
func Ingress(ing networkingv1.Ingress, defaultSecretNamespace, defaultSecretName string) []model.TLSListener {

	insecureListenerMap := make(map[string]model.HTTPListener)

	sourceResource := model.FullyQualifiedResource{
		Name:      ing.Name,
		Namespace: ing.Namespace,
		Group:     "",
		Version:   "v1",
		Kind:      "Ingress",
		UID:       string(ing.UID),
	}

	if ing.Spec.DefaultBackend != nil {
		backend := model.Backend{}
		backend.Name = ing.Spec.DefaultBackend.Service.Name
		backend.Namespace = ing.Namespace

		backend.Port = &model.BackendPort{}

		if ing.Spec.DefaultBackend.Service.Port.Name != "" {
			backend.Port.Name = ing.Spec.DefaultBackend.Service.Port.Name
		}

		if ing.Spec.DefaultBackend.Service.Port.Number != 0 {
			backend.Port.Port = uint32(ing.Spec.DefaultBackend.Service.Port.Number)
		}

		l := model.HTTPListener{
			Hostname: "*",
			Routes: []model.HTTPRoute{
				{
					Backends: []model.Backend{
						backend,
					},
				},
			},
			Port:    80,
			Service: getService(ing),
		}

		l.Sources = model.AddSource(l.Sources, sourceResource)

		insecureListenerMap["*"] = l
	}

	for _, rule := range ing.Spec.Rules {
		host := "*"
		if rule.Host != "" {
			host = rule.Host
		}

		l, ok := insecureListenerMap[host]
		l.Port = 80
		l.Sources = model.AddSource(l.Sources, sourceResource)
		if !ok {
			l.Name = "ing-" + ing.Name + "-" + ing.Namespace + "-" + host
		}

		l.Hostname = host
		if rule.HTTP == nil {
			log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
				Warn("Invalid Ingress rule without spec.rules.HTTP defined, skipping rule")
			continue
		}

		for _, path := range rule.HTTP.Paths {

			route := model.HTTPRoute{}

			switch *path.PathType {
			case networkingv1.PathTypeExact:
				route.PathMatch.Exact = path.Path
			case networkingv1.PathTypePrefix:
				route.PathMatch.Prefix = path.Path
			case networkingv1.PathTypeImplementationSpecific:
				route.PathMatch.Regex = path.Path
			}

			backend := model.Backend{
				Name:      path.Backend.Service.Name,
				Namespace: ing.Namespace,
			}
			if path.Backend.Service != nil {
				backend.Port = &model.BackendPort{}
				if path.Backend.Service.Port.Name != "" {
					backend.Port.Name = path.Backend.Service.Port.Name
				}
				if path.Backend.Service.Port.Number != 0 {
					backend.Port.Port = uint32(path.Backend.Service.Port.Number)
				}
			}
			route.Backends = append(route.Backends, backend)
			l.Routes = append(l.Routes, route)
			l.Service = getService(ing)
		}

		insecureListenerMap[host] = l
	}

	secureListenerMap := make(map[string]model.HTTPListener)

	for _, tlsConfig := range ing.Spec.TLS {
		for _, host := range tlsConfig.Hosts {

			l, ok := secureListenerMap[host]
			if !ok {
				l, ok = insecureListenerMap[host]
				if !ok {
					l, ok = insecureListenerMap["*"]
					if !ok {
						continue
					}
				}
			}

			if tlsConfig.SecretName != "" {
				l.TLS = []model.TLSSecret{
					{
						Name:      tlsConfig.SecretName,
						Namespace: ing.Namespace,
					},
				}
			} else if defaultSecretNamespace != "" && defaultSecretName != "" {
				l.TLS = []model.TLSSecret{
					{
						Name:      defaultSecretName,
						Namespace: defaultSecretNamespace,
					},
				}
			}

			l.Port = 443
			l.Hostname = host
			l.Service = getService(ing)
			secureListenerMap[host] = l

			defaultListener, ok := insecureListenerMap["*"]
			if ok {
				if tlsConfig.SecretName != "" {
					defaultListener.TLS = []model.TLSSecret{
						{
							Name:      tlsConfig.SecretName,
							Namespace: ing.Namespace,
						},
					}
				} else if defaultSecretNamespace != "" && defaultSecretName != "" {
					defaultListener.TLS = []model.TLSSecret{
						{
							Name:      defaultSecretName,
							Namespace: defaultSecretNamespace,
						},
					}
				}
				defaultListener.Hostname = host
				defaultListener.Port = 443
				secureListenerMap[host] = defaultListener

			}
		}
	}
	listenerSlice := make([]model.HTTPListener, 0, len(insecureListenerMap)+len(secureListenerMap))
	listenerSlice = appendValuesInKeyOrder(insecureListenerMap, listenerSlice)
	listenerSlice = appendValuesInKeyOrder(secureListenerMap, listenerSlice)
	return listenerSlice
}

// translate ingress with tls pass through into TLS listener
func IngressPassthrough(ing networkingv1.Ingress, defaultSecretNamespace, defaultSecretName string) []model.TLSListener {

	tlsListenerMap := make(map[string]model.TLSListener)

	sourceResource := model.FullyQualifiedResource{
		Name:      ing.Name,
		Namespace: ing.Namespace,
		Group:     "",
		Version:   "v1",
		Kind:      "Ingress",
		UID:       string(ing.UID),
	}

	if ing.Spec.DefaultBackend != nil {
		log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
			Warn("Invalid SSL Passthrough Ingress rule with a default backend, skipping default backend config")
	}

	for _, rule := range ing.Spec.Rules {
		if rule.Host == "" {
			log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
				Warn("Invalid SSL Passthrough Ingress rule without spec.rules.host defined, skipping rule")
			continue
		}

		host := rule.Host
		l, ok := tlsListenerMap[host]
		l.Port = 443
		l.Sources = model.AddSource(l.Sources, sourceResource)
		if !ok {
			l.Name = "ing-" + ing.Name + "-" + ing.Namespace + "-" + host
		}

		l.Hostname = host

		if rule.HTTP == nil {
			log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
				Warn("Invalid SSL Passthrough Ingress rule without spec.rules.HTTP defined, skipping rule")
			continue
		}

		for _, path := range rule.HTTP.Paths {
			if path.Path != "/" {
				log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
					Warn("Invalid SSL Passthrough Ingress rule with path not equal to '/', skipping rule")
				continue
			}

			route := model.TLSRoute{}

			backend := model.Backend{
				Name:      path.Backend.Service.Name,
				Namespace: ing.Namespace,
			}
			if path.Backend.Service != nil {
				backend.Port = &model.BackendPort{}
				if path.Backend.Service.Port.Name != "" {
					backend.Port.Name = path.Backend.Service.Port.Name
				}
				if path.Backend.Service.Port.Number != 0 {
					backend.Port.Port = uint32(path.Backend.Service.Port.Number)
				}
			}
			route.Backends = append(route.Backends, backend)
			l.Routes = append(l.Routes, route)
			l.Service = getService(ing)
		}

		if len(l.Routes) == 0 {
			log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name).
				Warn("Invalid SSL Passthrough Ingress with no valid rules, skipping")
			continue
		}

		tlsListenerMap[host] = l
	}

	listenerSlice := make([]model.TLSListener, 0, len(tlsListenerMap))
	listenerSlice = appendValuesInKeyOrder(tlsListenerMap, listenerSlice)

	return listenerSlice
}

func getService(ing networkingv1.Ingress) *model.Service {
	if annotations.GetAnnotationServiceType(&ing) != string(corev1.ServiceTypeNodePort) {
		return nil
	}

	m := &model.Service{
		Type: string(corev1.ServiceTypeNodePort),
	}
	scopedLog := log.WithField(logfields.Ingress, ing.Namespace+"/"+ing.Name)
	secureNodePort, err := annotations.GetAnnotationSecureNodePort(&ing)
	if err != nil {
		scopedLog.WithError(err).Warn("Invalid secure node port annotation, random port will be used")
	} else {
		m.SecureNodePort = secureNodePort
	}

	insureNodePort, err := annotations.GetAnnotationInsecureNodePort(&ing)
	if err != nil {
		scopedLog.WithError(err).Warn("Invalid insecure node port annotation, random port will be used")
	} else {
		m.InsecureNodePort = insureNodePort
	}

	return m
}
