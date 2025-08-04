// forum/handlers.go
package forum

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const PageSize = 50

// PaginationData holds all the necessary info for rendering pagination controls.
type PaginationData struct {
	CurrentPage int
	TotalPages  int
	NextPage    int
	PrevPage    int
	HasNext     bool
	HasPrev     bool
}

// TopicsViewData is the data structure for the topics list page.
type TopicsViewData struct {
	Topics      []Topic
	Pagination  PaginationData
	SearchQuery string
}

// TopicViewData is the data structure for the single topic page.
type TopicViewData struct {
	Topic      Topic
	Posts      []Post
	Pagination PaginationData
}

type Handlers struct {
	db        *Database
	templates *template.Template
}

func NewHandlers(db *Database) (*Handlers, error) {
	tpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Handlers{db: db, templates: tpl}, nil
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/topics", h.handleTopics) // Use the new multiplexer
	mux.HandleFunc("/topics/", h.showTopic)
}

// handleTopics acts as a router for the /topics endpoint based on the HTTP method.
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

// listTopics handles GET requests for searching and paginating all topics.
func (h *Handlers) listTopics(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	searchQuery := r.URL.Query().Get("q")

	topics, err := h.db.SearchAndListTopics(searchQuery, page, PageSize)
	if err != nil {
		log.Printf("Error searching topics: %v", err)
		http.Error(w, "Failed to retrieve topics", http.StatusInternalServerError)
		return
	}

	totalTopics, err := h.db.CountTopics(searchQuery)
	if err != nil {
		log.Printf("Error counting topics: %v", err)
		http.Error(w, "Failed to retrieve topics", http.StatusInternalServerError)
		return
	}

	totalPages := (totalTopics + PageSize - 1) / PageSize
	data := TopicsViewData{
		Topics:      topics,
		SearchQuery: searchQuery,
		Pagination: PaginationData{
			CurrentPage: page,
			TotalPages:  totalPages,
			NextPage:    page + 1,
			PrevPage:    page - 1,
			HasNext:     page < totalPages,
			HasPrev:     page > 1,
		},
	}

	err = h.templates.ExecuteTemplate(w, "topics.html", data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
	}
}

// showTopic handles viewing a single topic and paginating its posts.
func (h *Handlers) showTopic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/topics/")
	parts := strings.Split(path, "/")
	topicIDStr := parts[0]

	// Handle post creation
	if len(parts) == 2 && parts[1] == "posts" {
		if r.Method == http.MethodPost {
			h.createPost(w, r, topicIDStr)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Handle topic viewing
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	topicID, err := uuid.Parse(topicIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	topic, err := h.db.GetTopic(topicID)
	if err != nil {
		log.Printf("Error getting topic: %v", err)
		http.NotFound(w, r)
		return
	}

	posts, err := h.db.GetPostsByTopic(topicID, page, PageSize)
	if err != nil {
		log.Printf("Error getting posts: %v", err)
		http.Error(w, "Failed to retrieve posts", http.StatusInternalServerError)
		return
	}

	totalPosts, err := h.db.CountPostsByTopic(topicID)
	if err != nil {
		log.Printf("Error counting posts: %v", err)
		http.Error(w, "Failed to retrieve posts", http.StatusInternalServerError)
		return
	}

	totalPages := (totalPosts + PageSize - 1) / PageSize
	data := TopicViewData{
		Topic: *topic,
		Posts: posts,
		Pagination: PaginationData{
			CurrentPage: page,
			TotalPages:  totalPages,
			NextPage:    page + 1,
			PrevPage:    page - 1,
			HasNext:     page < totalPages,
			HasPrev:     page > 1,
		},
	}

	err = h.templates.ExecuteTemplate(w, "topic.html", data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
	}
}

func (h *Handlers) createPost(w http.ResponseWriter, r *http.Request, topicIDStr string) {
	topicID, err := uuid.Parse(topicIDStr)
	if err != nil {
		http.Error(w, "Invalid topic ID", http.StatusBadRequest)
		return
	}
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
	http.Redirect(w, r, "/topics/"+topicIDStr, http.StatusSeeOther)
}

// createTopic handles API requests to create a new topic from a JSON payload.
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
