package runtime

import "time"

type RunInput struct {
	ContainerName string
	NodeID        string
	ExecutionID   string
	PlanID        string
	ServiceID     string
	Image         string
	Command       []string
	Args          []string
	Env           map[string]string
	ContainerPort int
	HostBindIP    string
	HostPortMin   int
	HostPortMax   int
}

type RunResult struct {
	ContainerID   string
	ContainerName string
	HostPort      int
}

type LogRecord struct {
	Timestamp time.Time
	Stream    string
	Line      string
}

type LogEmitter func(LogRecord)
