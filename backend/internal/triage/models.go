package triage

import "encoding/json"

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (priority Priority) Valid() bool {
	return priority == PriorityLow || priority == PriorityMedium ||
		priority == PriorityHigh || priority == PriorityUrgent
}

func (priority Priority) Rank() int {
	switch priority {
	case PriorityLow:
		return 1
	case PriorityMedium:
		return 2
	case PriorityHigh:
		return 3
	case PriorityUrgent:
		return 4
	default:
		return 0
	}
}

type Assignee struct {
	ID            string  `json:"id"`
	Name          *string `json:"name,omitempty"`
	Email         string  `json:"email"`
	AvatarVersion *string `json:"avatar_version,omitempty"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func DecodeTags(payload []byte) ([]Tag, error) {
	tags := make([]Tag, 0)
	if len(payload) == 0 {
		return tags, nil
	}
	if err := json.Unmarshal(payload, &tags); err != nil {
		return nil, err
	}
	return tags, nil
}
