// forum/handlers.go
package forum

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Handlers holds dependencies for the forum's HTTP handlers.
type Handlers struct {
	db        *Database
	templates *template.Template
}

// NewHandlers creates a new Handlers instance and parses the HTML templates.
func NewHandlers(db *Database) (*Handlers, error) {
	// Parse all .html files in the templates directory.
	// The template name will be its filename (e.g., "topic.html").
	tpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Handlers{
		db:        db,
		templates: tpl,
	}, nil
}

// RegisterRoutes sets up the routing for the forum API.
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/topics", h.handleTopics)
	mux.HandleFunc("/topics/", h.handleTopicAndPosts) // Catches /topics/{id} and /topics/{id}/posts
}

// handleTopics handles requests for the /topics endpoint.
// GET: Shows a list of all topics.
// POST: Creates a new topic (API only).
func (h *Handlers) handleTopics(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTopics(w, r)
	case http.MethodPost:
		h.createTopic(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTopicAndPosts is a multiplexer for routes under /topics/{id}
func (h *Handlers) handleTopicAndPosts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/topics/")
	parts := strings.Split(path, "/")

	if len(parts) == 1 { // Route: /topics/{id}
		if r.Method == http.MethodGet {
			h.showTopic(w, r, parts[0])
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "posts" { // Route: /topics/{id}/posts
		if r.Method == http.MethodPost {
			h.createPost(w, r, parts[0])
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	} else {
		http.NotFound(w, r)
	}
}

// listTopics fetches all topics and renders the topics.html template.
func (h *Handlers) listTopics(w http.ResponseWriter, r *http.Request) {
	topics, err := h.db.GetAllTopics()
	if err != nil {
		log.Printf("Error getting all topics: %v", err)
		http.Error(w, "Failed to retrieve topics", http.StatusInternalServerError)
		return
	}

	err = h.templates.ExecuteTemplate(w, "topics.html", topics)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

// TopicViewData holds all the data needed for the topic view template.
type TopicViewData struct {
	Topic     Topic
	Posts     []Post
	UserEmail string
}

// showTopic fetches a single topic and its posts, then renders the topic.html template.
func (h *Handlers) showTopic(w http.ResponseWriter, r *http.Request, topicIDStr string) {
	topicID, err := uuid.Parse(topicIDStr)
	if err != nil {
		http.Error(w, "Invalid topic ID", http.StatusBadRequest)
		return
	}

	topic, err := h.db.GetTopic(topicID)
	if err != nil {
		log.Printf("Error getting topic: %v", err)
		http.NotFound(w, r)
		return
	}

	posts, err := h.db.GetPostsByTopic(topicID)
	if err != nil {
		log.Printf("Error getting posts for topic %s: %v", topicID, err)
		http.Error(w, "Failed to retrieve posts", http.StatusInternalServerError)
		return
	}

	data := TopicViewData{
		Topic: *topic,
		Posts: posts,
	}

	err = h.templates.ExecuteTemplate(w, "topic.html", data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

// createTopic handles API requests to create a new topic.
func (h *Handlers) createTopic(w http.ResponseWriter, r *http.Request) {
	var topic Topic
	if err := json.NewDecoder(r.Body).Decode(&topic); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if topic.ID == uuid.Nil || topic.Title == "" {
		http.Error(w, "Missing topic ID or title", http.StatusBadRequest)
		return
	}

	if topic.Tags == nil {
		topic.Tags = []string{}
	}

	if err := h.db.CreateTopic(&topic); err != nil {
		log.Printf("Error creating topic: %v", err)
		http.Error(w, "Failed to create topic", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(topic)
}

// createPost handles form submissions to add a new post to a topic.
func (h *Handlers) createPost(w http.ResponseWriter, r *http.Request, topicIDStr string) {
	topicID, err := uuid.Parse(topicIDStr)
	if err != nil {
		http.Error(w, "Invalid topic ID", http.StatusBadRequest)
		return
	}

	// We need to parse the form data from the request.
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	post := Post{
		TopicID: topicID,
		Author:  r.FormValue("author"),
		Body:    r.FormValue("body"),
	}

	if post.Author == "" || post.Body == "" {
		http.Error(w, "Author and body are required fields", http.StatusBadRequest)
		return
	}

	if err := h.db.CreatePost(&post); err != nil {
		log.Printf("Error creating post: %v", err)
		http.Error(w, "Failed to create post", http.StatusInternalServerError)
		return
	}

	// Redirect the user back to the topic page to see the new post.
	http.Redirect(w, r, "/topics/"+topicIDStr, http.StatusSeeOther)
}
