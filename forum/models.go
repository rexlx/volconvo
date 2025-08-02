// forum/models.go
package forum

import (
	"time"

	"github.com/google/uuid"
)

// Topic represents the main subject of a conversation.
// The `db` tags are no longer used but are harmless to keep.
type Topic struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Title     string    `json:"title" db:"title"`
	Tags      []string  `json:"tags" db:"tags"` // Back to native []string
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Post represents a single message within a Topic.
type Post struct {
	ID        int64     `json:"id" db:"id"`
	TopicID   uuid.UUID `json:"topic_id" db:"topic_id"`
	Author    string    `json:"author" db:"author"`
	Body      string    `json:"body" db:"body"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
