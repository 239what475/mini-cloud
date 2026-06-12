package nodeprovider

import (
	"context"
)

type CreateRequest struct {
	Name        string
	ClientToken string
	CPUMilli    int
	MemoryMi    int
}

type CreateResult struct {
	InstanceID   string
	InstanceName string
	InstanceType string
}

type DeleteRequest struct {
	InstanceID string
}

type Driver interface {
	Create(ctx context.Context, request CreateRequest) (CreateResult, error)
	Delete(ctx context.Context, request DeleteRequest) error
}
