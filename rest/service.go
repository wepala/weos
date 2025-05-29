package rest

type Service interface {
	// GetName returns the name of the provider (e.g. shopify, amazon)
	GetName() string
	// GetSupportedReports return the supported reports for the provider
	GetSupportedReports() []string
}

type ServiceRegistry interface {
	GetService(name string) Service
	AddService(service Service) ServiceRegistry
}
