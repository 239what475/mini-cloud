package model

type Route struct {
	Host     string
	Backends []string
}

type RouteSource struct {
	ServiceName string
	Host        string
	NodeID      string
	HostPort    int
	HasBackend  bool
}

type FrontDoorDNSRecord struct {
	Subdomain string
	Type      string
	Value     string
	Action    string
}

type ManagedFrontDoorDomain struct {
	Host         string
	CNAME        string
	Verification *FrontDoorDNSRecord
}
