// forum/database.go
package forum

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema contains the SQL statements to create the necessary tables and extensions.
const schema = `
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE TABLE IF NOT EXISTS topics (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS posts (
    id SERIAL PRIMARY KEY,
    topic_id UUID NOT NULL,
    author TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_topic
        FOREIGN KEY(topic_id)
        REFERENCES topics(id)
        ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_posts_on_topic_id ON posts(topic_id);
`

// Database now holds a pgxpool.Pool directly.
type Database struct {
	pool *pgxpool.Pool
}

// NewDatabase creates a new Database instance and connects using a pgxpool.
func NewDatabase(connectionString string) (*Database, error) {
	pool, err := pgxpool.New(context.Background(), connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Database{pool: pool}, nil
}

// CreateTables executes the schema definition to create tables if they don't exist.
func (d *Database) CreateTables() error {
	_, err := d.pool.Exec(context.Background(), schema)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}
	return nil
}

// CreateTopic inserts a new topic into the database.
func (d *Database) CreateTopic(topic *Topic) error {
	query := `INSERT INTO topics (id, title, tags) VALUES ($1, $2, $3) RETURNING created_at`
	return d.pool.QueryRow(context.Background(), query, topic.ID, topic.Title, topic.Tags).Scan(&topic.CreatedAt)
}

// GetTopic retrieves a single topic by its UUID.
func (d *Database) GetTopic(id uuid.UUID) (*Topic, error) {
	var topic Topic
	query := `SELECT id, title, tags, created_at FROM topics WHERE id = $1`
	row := d.pool.QueryRow(context.Background(), query, id)
	err := row.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &topic, nil
}

// GetAllTopics retrieves all topics from the database, ordered by most recent.
func (d *Database) GetAllTopics() ([]Topic, error) {
	topics := make([]Topic, 0)
	query := `SELECT id, title, tags, created_at FROM topics ORDER BY created_at DESC`

	rows, err := d.pool.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var topic Topic
		if err := rows.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, topic)
	}

	return topics, nil
}

// CreatePost inserts a new post into the database, linked to a topic.
func (d *Database) CreatePost(post *Post) error {
	query := `INSERT INTO posts (topic_id, author, body) VALUES ($1, $2, $3) RETURNING id, created_at`
	return d.pool.QueryRow(context.Background(), query, post.TopicID, post.Author, post.Body).Scan(&post.ID, &post.CreatedAt)
}

// GetPostsByTopic retrieves all posts for a given topic, ordered by creation time.
func (d *Database) GetPostsByTopic(topicID uuid.UUID) ([]Post, error) {
	posts := make([]Post, 0)
	query := `SELECT id, topic_id, author, body, created_at FROM posts WHERE topic_id = $1 ORDER BY created_at ASC`

	rows, err := d.pool.Query(context.Background(), query, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.TopicID, &p.Author, &p.Body, &p.CreatedAt); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}

	// After the loop, check for any error that occurred during iteration.
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return posts, nil
}
