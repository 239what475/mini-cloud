package store

import (
	"context"
	"fmt"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func (s *Store) GetAlertSignal(ctx context.Context) (cloudmodel.AlertSignal, error) {
	var executionFailures bool
	var stuckExecutions bool
	var offlineNodes bool
	var bootstrapStuck bool
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				COUNT(*) FILTER (WHERE status = 'pending')::int AS pending_count,
				COUNT(*) FILTER (WHERE status = 'deploying')::int AS deploying_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
				MAX(updated_at) AS updated_at
			FROM execution_intents
			GROUP BY plan_id
		)
		SELECT
			EXISTS (
				SELECT 1
				FROM plan_counts
				WHERE failed_count > 0
			),
			EXISTS (
				SELECT 1
				FROM plan_counts
				WHERE failed_count = 0
				  AND (pending_count > 0 OR deploying_count > 0)
				  AND updated_at <= now() - ($1 * interval '1 second')
			),
			EXISTS (
				SELECT 1
				FROM nodes
				WHERE status = 'offline'
			),
			EXISTS (
				SELECT 1
				FROM nodes
				WHERE status = 'provisioning'
				  AND created_at <= now() - ($2 * interval '1 second')
			)
	`, cloudmodel.ExecutionPlanStuckThresholdSeconds, cloudmodel.NodeRegistrationTimeoutSeconds).Scan(
		&executionFailures,
		&stuckExecutions,
		&offlineNodes,
		&bootstrapStuck,
	); err != nil {
		return cloudmodel.AlertSignal{}, fmt.Errorf("query alert signal: %w", err)
	}

	out := cloudmodel.AlertSignal{}
	for _, firing := range []bool{executionFailures, stuckExecutions, offlineNodes, bootstrapStuck} {
		if firing {
			out.AlertsFiring++
		}
	}
	return out, nil
}
