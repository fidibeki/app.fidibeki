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

type Assignee struct {
	ID    string  `json:"id"`
	Name  *string `json:"name,omitempty"`
	Email string  `json:"email"`
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
