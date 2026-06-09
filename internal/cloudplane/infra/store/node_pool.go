package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

type NodeScaleOutCandidate struct {
	PlanID          string
	ServiceID       string
	CPUMilli        int
	MemoryMi        int
	NodeName        string
	InstanceType    string
	ClientToken     string
	HasCapacity     bool
	HasProvisioning bool
}

func (s *Store) GetNodeScaleOutCandidate(ctx context.Context, nodeNamePrefix string, instanceType string) (*NodeScaleOutCandidate, error) {
	var candidate NodeScaleOutCandidate
	err := s.db.QueryRowContext(ctx, `
		WITH pending AS (
			SELECT
				plan_id,
				service_id,
				cpu_milli_request,
				memory_mi_request,
				created_at
			FROM execution_intents
			WHERE work_action = $1
			  AND status = $2
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		),
		capacity AS (
			SELECT EXISTS (
				SELECT 1
				FROM nodes, pending
				WHERE status = $3
				  AND schedulable
				  AND cpu_milli_allocatable - cpu_milli_allocated >= pending.cpu_milli_request
				  AND memory_mi_allocatable - memory_mi_allocated >= pending.memory_mi_request
			) AS has_capacity
		),
		provisioning AS (
			SELECT EXISTS (
				SELECT 1
				FROM nodes
				WHERE status = $4
			) AS has_provisioning
		)
		SELECT
			pending.plan_id,
			pending.service_id,
			pending.cpu_milli_request,
			pending.memory_mi_request,
			$5 || '-' || lower(substr(md5(pending.plan_id), 1, 10)),
			$6,
			pending.plan_id,
			capacity.has_capacity,
			provisioning.has_provisioning
		FROM pending
		CROSS JOIN capacity
		CROSS JOIN provisioning
	`, cloudmodel.WorkActionRun, cloudmodel.StatusPending, cloudmodel.StatusReady, cloudmodel.StatusProvisioning, nodeNamePrefix, instanceType).Scan(
		&candidate.PlanID,
		&candidate.ServiceID,
		&candidate.CPUMilli,
		&candidate.MemoryMi,
		&candidate.NodeName,
		&candidate.InstanceType,
		&candidate.ClientToken,
		&candidate.HasCapacity,
		&candidate.HasProvisioning,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query node scale-out candidate: %w", err)
	}
	return &candidate, nil
}

func (s *Store) HasUnsettledExecutionIntents(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM execution_intents
			WHERE status IN ($1, $2)
		)
	`, cloudmodel.StatusPending, cloudmodel.StatusDeploying).Scan(&exists); err != nil {
		return false, fmt.Errorf("check unsettled execution intents: %w", err)
	}
	return exists, nil
}

func (s *Store) MarkNodeDraining(ctx context.Context, nodeID string, reason string, observedAt time.Time) (cloudmodel.Node, bool, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("begin node draining tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	currentRow := tx.QueryRowContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE id = $1
		FOR UPDATE
	`, nodeID)
	current, err := scanNode(currentRow)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, false, ErrNodeNotFound
		}
		return cloudmodel.Node{}, false, fmt.Errorf("load node for draining: %w", err)
	}
	if current.Status == cloudmodel.StatusDraining {
		return current, true, nil
	}
	if current.Status != cloudmodel.StatusReady || strings.TrimSpace(current.InstanceID) == "" {
		return current, false, nil
	}

	var activeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM execution_intents
		WHERE node_id = $1
		  AND status IN ($2, $3)
	`, current.ID, cloudmodel.StatusDeploying, cloudmodel.StatusRunning).Scan(&activeCount); err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("count active executions before node draining: %w", err)
	}
	if activeCount > 0 {
		return current, false, nil
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			status = $2,
			status_reason = $3,
			schedulable = FALSE,
			updated_at = $4
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, nodeID, cloudmodel.StatusDraining, reason, observedAt.UTC())
	item, err := scanNode(row)
	if err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("mark node draining: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("commit node draining tx: %w", err)
	}
	return item, true, nil
}

func (s *Store) MarkNodeDeleted(ctx context.Context, nodeID string, reason string, observedAt time.Time) (cloudmodel.Node, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			status = $2,
			status_reason = $3,
			schedulable = FALSE,
			cpu_milli_allocated = 0,
			memory_mi_allocated = 0,
			session_token_prefix = '',
			session_token_hash = NULL,
			session_last_used_at = NULL,
			session_expires_at = NULL,
			updated_at = $4
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, nodeID, cloudmodel.StatusDeleted, reason, observedAt.UTC())
	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, ErrNodeNotFound
		}
		return cloudmodel.Node{}, fmt.Errorf("mark node deleted: %w", err)
	}
	return item, nil
}
