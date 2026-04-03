package models

import (
	"encoding/json"
	"time"

	"weos/domain/entities"
)

// ResourcePermission is the GORM model for instance-level resource permissions.
type ResourcePermission struct {
	ID         string `gorm:"primaryKey"`
	ResourceID string `gorm:"not null;uniqueIndex:idx_rp_resource_agent"`
	AgentID    string `gorm:"not null;uniqueIndex:idx_rp_resource_agent"`
	Actions    string `gorm:"type:text"` // JSON array: ["read","modify"]
	GrantedBy  string `gorm:"not null"`
	GrantedAt  time.Time
}

func (m ResourcePermission) TableName() string {
	return "resource_permissions"
}

func (m *ResourcePermission) ToEntity() (*entities.ResourcePermission, error) {
	var actions []string
	if m.Actions != "" {
		if err := json.Unmarshal([]byte(m.Actions), &actions); err != nil {
			return nil, err
		}
	}
	return &entities.ResourcePermission{
		ID:         m.ID,
		ResourceID: m.ResourceID,
		AgentID:    m.AgentID,
		Actions:    actions,
		GrantedBy:  m.GrantedBy,
		GrantedAt:  m.GrantedAt,
	}, nil
}

func FromResourcePermission(e *entities.ResourcePermission) (*ResourcePermission, error) {
	actionsJSON, err := json.Marshal(e.Actions)
	if err != nil {
		return nil, err
	}
	return &ResourcePermission{
		ID:         e.ID,
		ResourceID: e.ResourceID,
		AgentID:    e.AgentID,
		Actions:    string(actionsJSON),
		GrantedBy:  e.GrantedBy,
		GrantedAt:  e.GrantedAt,
	}, nil
}
