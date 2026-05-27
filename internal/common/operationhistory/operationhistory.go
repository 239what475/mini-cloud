package operationhistory

import "time"

const (
	ResultSucceeded = "succeeded"
	ResultDenied    = "denied"
	ResultFailed    = "failed"
)

type Record struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"projectID"`
	Action         string         `json:"action"`
	TargetType     string         `json:"targetType"`
	TargetID       string         `json:"targetID"`
	TargetName     string         `json:"targetName"`
	ActorKind      string         `json:"actorKind"`
	ActorID        string         `json:"actorID"`
	ActorLabel     string         `json:"actorLabel"`
	ActorProjectID string         `json:"actorProjectID"`
	RequestMethod  string         `json:"requestMethod"`
	RequestPath    string         `json:"requestPath"`
	Result         string         `json:"result"`
	Details        map[string]any `json:"details"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type CreateInput struct {
	ProjectID      string
	Action         string
	TargetType     string
	TargetID       string
	TargetName     string
	ActorKind      string
	ActorID        string
	ActorLabel     string
	ActorProjectID string
	RequestMethod  string
	RequestPath    string
	Result         string
	Details        map[string]any
}
