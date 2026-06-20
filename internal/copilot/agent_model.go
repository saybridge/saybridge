package copilot

import "time"

// AIAgent represents a specialized AI assistant in the Multi-Agent system.
type AIAgent struct {
	ID             string    `gorm:"primaryKey;type:varchar(50)" json:"id"`
	Name           string    `gorm:"type:varchar(100);not null" json:"name"`
	Username       string    `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	Avatar         string    `gorm:"type:varchar(50)" json:"avatar"` // Emoji or icon identifier
	SystemPrompt   string    `gorm:"type:text;not null" json:"systemPrompt"`
	Model          string    `gorm:"type:varchar(50)" json:"model"`
	Temperature    float64   `gorm:"type:double precision;default:0.7" json:"temperature"`
	TriggerType    string    `gorm:"type:varchar(20);default:'mention'" json:"triggerType"` // "mention", "silent"
	TriggerKeyword string    `gorm:"type:varchar(50)" json:"triggerKeyword"` // e.g. "@sai:coder"
	RoomIDs        string    `gorm:"type:text" json:"roomIds"` // comma-separated room IDs for silent integration
	Enabled        bool      `gorm:"type:boolean;default:true" json:"enabled"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (AIAgent) TableName() string {
	return "ai_agents"
}
