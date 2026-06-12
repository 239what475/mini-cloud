package model

type Route struct {
	Host     string
	Backends []string
}

type RouteSource struct {
	ServiceName string
	NodeID      string
	HostPort    int
	HasBackend  bool
}

type FrontDoorDNSRecord struct {
	ID        uint64
	Subdomain string
	Type      string
	Value     string
}

type ManagedFrontDoorDomain struct {
	Host         string
	CNAME        string
	Verification *FrontDoorDNSRecord
}
