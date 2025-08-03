// forum/database.go
package forum

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema remains the same.
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

type Database struct {
	pool *pgxpool.Pool
}

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

func (d *Database) CreateTables() error {
	_, err := d.pool.Exec(context.Background(), schema)
	return err
}

// --- Topic Functions ---

func (d *Database) CreateTopic(topic *Topic) error {
	query := `INSERT INTO topics (id, title, tags) VALUES ($1, $2, $3) RETURNING created_at`
	return d.pool.QueryRow(context.Background(), query, topic.ID, topic.Title, topic.Tags).Scan(&topic.CreatedAt)
}

func (d *Database) GetTopic(id uuid.UUID) (*Topic, error) {
	var topic Topic
	query := `SELECT id, title, tags, created_at FROM topics WHERE id = $1`
	row := d.pool.QueryRow(context.Background(), query, id)
	err := row.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt)
	return &topic, err
}

// SearchAndListTopics retrieves a paginated list of topics, with an optional search query.
func (d *Database) SearchAndListTopics(searchQuery string, page, pageSize int) ([]Topic, error) {
	offset := (page - 1) * pageSize

	// Base query
	query := "SELECT id, title, tags, created_at FROM topics"
	args := []interface{}{}

	// Add search condition if a query is provided
	if searchQuery != "" {
		query += " WHERE title ILIKE $1 OR $2 = ANY(tags)"
		args = append(args, "%"+searchQuery+"%", strings.ToLower(searchQuery))
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC LIMIT $%d OFFSET $%d"
	query = fmt.Sprintf(query, len(args)+1, len(args)+2)
	args = append(args, pageSize, offset)

	rows, err := d.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []Topic
	for rows.Next() {
		var topic Topic
		if err := rows.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, topic)
	}
	return topics, rows.Err()
}

// CountTopics returns the total number of topics, with an optional search query.
func (d *Database) CountTopics(searchQuery string) (int, error) {
	query := "SELECT COUNT(*) FROM topics"
	args := []interface{}{}

	if searchQuery != "" {
		query += " WHERE title ILIKE $1 OR $2 = ANY(tags)"
		args = append(args, "%"+searchQuery+"%", strings.ToLower(searchQuery))
	}

	var count int
	err := d.pool.QueryRow(context.Background(), query, args...).Scan(&count)
	return count, err
}

// --- Post Functions ---

func (d *Database) CreatePost(post *Post) error {
	query := `INSERT INTO posts (topic_id, author, body) VALUES ($1, $2, $3) RETURNING id, created_at`
	return d.pool.QueryRow(context.Background(), query, post.TopicID, post.Author, post.Body).Scan(&post.ID, &post.CreatedAt)
}

// GetPostsByTopic retrieves a paginated list of posts for a given topic.
func (d *Database) GetPostsByTopic(topicID uuid.UUID, page, pageSize int) ([]Post, error) {
	offset := (page - 1) * pageSize
	query := `SELECT id, topic_id, author, body, created_at FROM posts 
	          WHERE topic_id = $1 
	          ORDER BY created_at ASC 
	          LIMIT $2 OFFSET $3`

	rows, err := d.pool.Query(context.Background(), query, topicID, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.TopicID, &p.Author, &p.Body, &p.CreatedAt); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// CountPostsByTopic returns the total number of posts for a given topic.
func (d *Database) CountPostsByTopic(topicID uuid.UUID) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM posts WHERE topic_id = $1"
	err := d.pool.QueryRow(context.Background(), query, topicID).Scan(&count)
	return count, err
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
