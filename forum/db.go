// forum/database.go
package forum

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The schema is updated to correctly match the User and Token models.
const schema = `
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE TABLE IF NOT EXISTS topics (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    author_id UUID NOT NULL
);
CREATE TABLE IF NOT EXISTS posts (
    id SERIAL PRIMARY KEY,
    topic_id UUID NOT NULL,
    author TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    author_id UUID NOT NULL,
    parent_post_id INTEGER,
    CONSTRAINT fk_topic
        FOREIGN KEY(topic_id)
        REFERENCES topics(id)
        ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    key TEXT NOT NULL UNIQUE,
    handle TEXT NOT NULL,
    hash BYTEA,
    password TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notifications JSONB NOT NULL DEFAULT '[]',
    admin BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE IF NOT EXISTS tokens (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    user_id UUID NOT NULL,
    token TEXT NOT NULL,
    handle TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    hash BYTEA NOT NULL
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
	query := `INSERT INTO topics (id, title, tags, author_id) VALUES ($1, $2, $3, $4) RETURNING created_at`
	return d.pool.QueryRow(context.Background(), query, topic.ID, topic.Title, topic.Tags, topic.AuthorID).Scan(&topic.CreatedAt)
}

func (d *Database) GetTopic(id uuid.UUID) (*Topic, error) {
	var topic Topic
	query := `SELECT id, title, tags, created_at, author_id FROM topics WHERE id = $1`
	row := d.pool.QueryRow(context.Background(), query, id)
	err := row.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt, &topic.AuthorID)
	if err == sql.ErrNoRows {
		return nil, nil // Return nil, nil for not found
	}
	return &topic, err
}

func (d *Database) SearchAndListTopics(searchQuery string, page, pageSize int) ([]Topic, error) {
	offset := (page - 1) * pageSize
	query := "SELECT id, title, tags, created_at, author_id FROM topics"
	args := []interface{}{}
	if searchQuery != "" {
		query += " WHERE title ILIKE $1 OR $2 = ANY(tags)"
		args = append(args, "%"+searchQuery+"%", strings.ToLower(searchQuery))
	}
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
		if err := rows.Scan(&topic.ID, &topic.Title, &topic.Tags, &topic.CreatedAt, &topic.AuthorID); err != nil {
			return nil, err
		}
		topics = append(topics, topic)
	}
	return topics, rows.Err()
}

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
	query := `INSERT INTO posts (topic_id, author, body, author_id, parent_post_id) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`
	return d.pool.QueryRow(context.Background(), query, post.TopicID, post.Author, post.Body, post.AuthorID, post.ParentPostID).Scan(&post.ID, &post.CreatedAt)
}

func (d *Database) GetPostsByTopic(topicID uuid.UUID, page, pageSize int) ([]Post, error) {
	offset := (page - 1) * pageSize
	query := `SELECT id, topic_id, author, body, created_at, author_id, parent_post_id FROM posts 
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
		if err := rows.Scan(&p.ID, &p.TopicID, &p.Author, &p.Body, &p.CreatedAt, &p.AuthorID, &p.ParentPostID); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func (d *Database) GetPost(id int64) (*Post, error) {
	var post Post
	query := `SELECT id, topic_id, author, body, created_at, author_id, parent_post_id FROM posts WHERE id = $1`
	row := d.pool.QueryRow(context.Background(), query, id)
	err := row.Scan(&post.ID, &post.TopicID, &post.Author, &post.Body, &post.CreatedAt, &post.AuthorID, &post.ParentPostID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &post, err
}

func (d *Database) CountPostsByTopic(topicID uuid.UUID) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM posts WHERE topic_id = $1"
	err := d.pool.QueryRow(context.Background(), query, topicID).Scan(&count)
	return count, err
}

// --- User and Token Functions ---

func (d *Database) SaveUser(user *User) error {
	notificationsJSON, err := json.Marshal(user.Notifications)
	if err != nil {
		return fmt.Errorf("failed to marshal notifications: %w", err)
	}

	query := `
        INSERT INTO users (id, email, key, handle, hash, password, created_at, updated_at, admin, notifications)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        ON CONFLICT (email) DO UPDATE SET
            key = EXCLUDED.key,
            handle = EXCLUDED.handle,
            hash = EXCLUDED.hash,
            password = EXCLUDED.password,
            updated_at = EXCLUDED.updated_at,
            admin = EXCLUDED.admin,
            notifications = EXCLUDED.notifications;
    `
	_, err = d.pool.Exec(context.Background(), query,
		user.ID,
		user.Email,
		user.Key,
		user.Handle,
		user.Hash,
		user.Password,
		user.Created,
		user.Updated,
		user.Admin,
		notificationsJSON,
	)
	return err
}

func (d *Database) SaveToken(token *Token) error {
	query := `
        INSERT INTO tokens (id, user_id, email, token, handle, created_at, expires_at, hash)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (id) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            email = EXCLUDED.email,
            token = EXCLUDED.token,
            handle = EXCLUDED.handle,
            created_at = EXCLUDED.created_at,
            expires_at = EXCLUDED.expires_at,
            hash = EXCLUDED.hash;
    `
	_, err := d.pool.Exec(context.Background(), query,
		token.ID,
		token.UserID,
		token.Email,
		token.Token,
		token.Handle,
		token.CreatedAt,
		token.ExpiresAt,
		token.Hash,
	)
	return err
}

func (d *Database) GetTokenByValue(value string) (*Token, error) {
	var token Token
	query := `
        SELECT id, user_id, email, token, handle, created_at, expires_at, hash
        FROM tokens
        WHERE token = $1`
	row := d.pool.QueryRow(context.Background(), query, value)
	err := row.Scan(
		&token.ID,
		&token.UserID,
		&token.Email,
		&token.Token,
		&token.Handle,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.Hash,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &token, nil
}

func (d *Database) GetUserByEmail(email string) (*User, error) {
	var user User
	var notificationsJSON []byte

	query := `
        SELECT id, email, key, handle, hash, password, created_at, updated_at, admin, notifications
        FROM users
        WHERE email = $1`

	row := d.pool.QueryRow(context.Background(), query, email)

	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Key,
		&user.Handle,
		&user.Hash,
		&user.Password,
		&user.Created,
		&user.Updated,
		&user.Admin,
		&notificationsJSON,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(notificationsJSON, &user.Notifications); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notifications: %w", err)
	}

	return &user, nil
}

// GetUserByID is required for the notification logic.
func (d *Database) GetUserByID(id string) (*User, error) {
	var user User
	var notificationsJSON []byte

	query := `
        SELECT id, email, key, handle, hash, password, created_at, updated_at, admin, notifications
        FROM users
        WHERE id = $1`

	row := d.pool.QueryRow(context.Background(), query, id)

	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Key,
		&user.Handle,
		&user.Hash,
		&user.Password,
		&user.Created,
		&user.Updated,
		&user.Admin,
		&notificationsJSON,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(notificationsJSON, &user.Notifications); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notifications: %w", err)
	}

	return &user, nil
}
