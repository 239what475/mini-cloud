package eventlog

import "time"

type Record struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetID"`
	TargetName string    `json:"targetName"`
	CreatedAt  time.Time `json:"createdAt"`
}

type CreateInput struct {
	Action     string
	TargetType string
	TargetID   string
	TargetName string
}
