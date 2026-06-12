package model

import "time"

const (
	ExecutionPlanStuckThresholdSeconds = int64((10 * time.Minute) / time.Second)
	NodeRegistrationTimeoutSeconds     = int64((10 * time.Minute) / time.Second)
)

type AlertSignal struct {
	AlertsFiring int
}
