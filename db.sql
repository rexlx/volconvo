-- db.sql
-- This script contains the schema for the forum tables in PostgreSQL.

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Topics are identified by a UUID, which will be provided by your main application.
CREATE TABLE IF NOT EXISTS topics (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}', -- Added tags column as a text array
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Posts belong to a topic and represent the conversation.
CREATE TABLE IF NOT EXISTS posts (
    id SERIAL PRIMARY KEY,
    topic_id UUID NOT NULL,
    author TEXT NOT NULL, -- In a real app, this might be a foreign key to a users table
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_topic
        FOREIGN KEY(topic_id)
        REFERENCES topics(id)
        ON DELETE CASCADE -- If a topic is deleted, all its posts are deleted too.
);

-- Create an index on topic_id for faster retrieval of posts for a given topic.
CREATE INDEX IF NOT EXISTS idx_posts_on_topic_id ON posts(topic_id);
