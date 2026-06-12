package model

import (
	"errors"
	"net"
	"strings"
	"time"
)

const (
	StatusProvisioning = "provisioning"
	StatusRegistering  = "registering"
	StatusReady        = "ready"
	StatusDraining     = "draining"
	StatusOffline      = "offline"
	StatusDeleted      = "deleted"
)

type Node struct {
	ID                  string
	Provider            string
	Region              string
	Name                string
	PrivateIP           string
	InstanceID          string
	InstanceType        string
	CPUMilliTotal       int
	MemoryMiTotal       int
	CPUMilliAllocatable int
	MemoryMiAllocatable int
	CPUMilliAllocated   int
	MemoryMiAllocated   int
	Status              string
	StatusReason        string
	Schedulable         bool
	Elastic             bool
	LastHeartbeatAt     *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ProvisioningInput struct {
	Provider     string
	Region       string
	Name         string
	InstanceType string
	StatusReason string
}

func (in ProvisioningInput) Validate() error {
	if strings.TrimSpace(in.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(in.Region) == "" {
		return errors.New("region is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(in.InstanceType) == "" {
		return errors.New("instanceType is required")
	}
	return nil
}

type RegisterInput struct {
	Provider      string
	Region        string
	Name          string
	PrivateIP     string
	InstanceID    string
	InstanceType  string
	CPUMilliTotal int
	MemoryMiTotal int
}

func (in RegisterInput) Validate() error {
	if strings.TrimSpace(in.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(in.Region) == "" {
		return errors.New("region is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(in.PrivateIP) == "" {
		return errors.New("privateIP is required")
	}
	if net.ParseIP(strings.TrimSpace(in.PrivateIP)) == nil {
		return errors.New("privateIP must be a valid IP address")
	}
	if strings.TrimSpace(in.InstanceID) == "" {
		return errors.New("instanceID is required")
	}
	if strings.TrimSpace(in.InstanceType) == "" {
		return errors.New("instanceType is required")
	}
	if in.CPUMilliTotal <= 0 {
		return errors.New("cpuMilliTotal must be greater than 0")
	}
	if in.MemoryMiTotal <= 0 {
		return errors.New("memoryMiTotal must be greater than 0")
	}
	return nil
}

type HeartbeatInput struct {
	CPUMilliAllocatable int
	MemoryMiAllocatable int
}

func (in HeartbeatInput) Validate() error {
	if in.CPUMilliAllocatable <= 0 {
		return errors.New("cpuMilliAllocatable must be greater than 0")
	}
	if in.MemoryMiAllocatable <= 0 {
		return errors.New("memoryMiAllocatable must be greater than 0")
	}
	return nil
}
