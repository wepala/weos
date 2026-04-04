package entities

import "time"

// ResourcePermission represents an explicit grant of actions on a specific
// resource to a specific agent. Not event-sourced — simple CRUD.
type ResourcePermission struct {
	ID         string
	ResourceID string
	AgentID    string
	Actions    []string // e.g. ["read", "modify", "delete"]
	GrantedBy  string
	GrantedAt  time.Time
}
