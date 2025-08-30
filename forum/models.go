// forum/models.go
package forum

import (
	"time"
)

// Topic now includes the ID of the user who created it, as a string.
type Topic struct {
	ID        string    `json:"id" db:"id"`
	Title     string    `json:"title" db:"title"`
	Tags      []string  `json:"tags" db:"tags"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	AuthorID  string    `json:"author_id" db:"author_id"` // Changed to string
}

// Post now includes the author's ID and parent post ID, using string for UUIDs.
type Post struct {
	ID           int64     `json:"id" db:"id"`
	TopicID      string    `json:"topic_id" db:"topic_id"` // Changed to string
	Author       string    `json:"author" db:"author"`
	Body         string    `json:"body" db:"body"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	AuthorID     string    `json:"author_id" db:"author_id"` // Changed to string
	ParentPostID *int64    `json:"parent_post_id" db:"parent_post_id"`
}
