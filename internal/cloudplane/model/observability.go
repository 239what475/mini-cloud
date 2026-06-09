// Package model 定义 cloud-plane 需要同步给 control-plane 的告警信号。
package model

import "time"

const (
	ExecutionPlanStuckThresholdSeconds = int64((10 * time.Minute) / time.Second)
	NodeRegistrationTimeoutSeconds     = int64((10 * time.Minute) / time.Second)
)

type AlertSignal struct {
	AlertsFiring int `json:"alertsFiring"`
}
