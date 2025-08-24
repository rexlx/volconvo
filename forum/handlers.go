// forum/handlers.go
package forum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

const PageSize = 50
const SessionCookieName = "forum_session_token"

// A contextKey is used to define a key for values stored in a request's context.
type contextKey string

const userContextKey = contextKey("token")

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
// It now includes the currently logged-in user.
type TopicsViewData struct {
	Topics      []Topic
	Pagination  PaginationData
	SearchQuery string
	User        *User
}

// TopicViewData is the data structure for the single topic page.
// It now includes the currently logged-in user.
type TopicViewData struct {
	Topic      Topic
	Posts      []Post
	Pagination PaginationData
	User       *User
}

// LoginViewData is used for the login page, to display potential errors.
type LoginViewData struct {
	Error string
}

type Handlers struct {
	Session   *scs.SessionManager `json:"-"`
	db        *Database
	templates *template.Template
}

func NewHandlers(db *Database) (*Handlers, error) {
	// Ensure all templates, including the new login.html, are parsed.
	tpl, err := template.ParseGlob("templates/*.html")

	if err != nil {
		return nil, err
	}

	sessionMgr := scs.New()
	sessionMgr.Lifetime = 24 * time.Hour
	sessionMgr.IdleTimeout = 1 * time.Hour
	sessionMgr.Cookie.Persist = true
	sessionMgr.Cookie.Name = "token"
	sessionMgr.Cookie.SameSite = http.SameSiteLaxMode
	sessionMgr.Cookie.Secure = true
	sessionMgr.Cookie.HttpOnly = true
	hndlr := &Handlers{
		Session:   sessionMgr,
		db:        db,
		templates: tpl,
	}
	return hndlr, nil
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("/api/user/create", h.addUserHandler)

	// Auth routes
	mux.HandleFunc("/login", h.handleLogin)
	mux.HandleFunc("/logout", h.handleLogout)

	// Wrap topic routes with authentication middleware
	mux.Handle("/topics", h.ValidateSessionToken(http.HandlerFunc(h.handleTopics)))
	mux.Handle("/topics/", h.ValidateSessionToken(http.HandlerFunc(h.showTopic)))
}

// addUserHandler creates a new user from a JSON payload.
func (h *Handlers) addUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Handle   string `json:"handle"`
		Admin    bool   `json:"admin"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" || req.Handle == "" {
		http.Error(w, "Email, password, and handle are required fields", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	existingUser, err := h.db.GetUserByEmail(req.Email)
	if err != nil {
		log.Printf("Error checking for existing user: %v", err)
		// http.Error(w, "Internal server error", http.StatusInternalServerError)
		// return
	}
	if existingUser != nil {
		http.Error(w, "User with this email already exists", http.StatusConflict)
		return
	}

	user, err := NewUser(req.Email, req.Admin)
	if err != nil {
		log.Printf("Error creating new user: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}
	user.Handle = req.Handle

	if err := user.SetPassword(req.Password); err != nil {
		log.Printf("Error setting password: %v", err)
		http.Error(w, "Failed to set password", http.StatusInternalServerError)
		return
	}

	if err := h.db.SaveUser(user); err != nil {
		log.Printf("Error saving user: %v", err)
		http.Error(w, "Failed to save user", http.StatusInternalServerError)
		return
	}

	user.Sanitize() // Remove password hash before sending response

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// authMiddleware checks for a valid session cookie and adds the user to the request context.
func (h *Handlers) ValidateSessionToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := h.GetTokenFromSession(r)
		if err != nil {
			token := r.Header.Get("Authorization")
			parts := strings.Split(token, ":")
			if token == "" || len(parts) != 2 {
				fmt.Println("token missing?", token)
				// For web pages, we don't want to error out, just proceed without a user.
				// We'll add a nil user to the context.
				ctx := context.WithValue(r.Context(), userContextKey, (*User)(nil))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			user, err := h.db.GetUserByEmail(parts[0])
			if err != nil {
				fmt.Println("Error getting user by email:", err, user, token)
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}
			if user.Key != parts[1] {
				fmt.Println("User key mismatch:", user.Key, parts[1], token, parts)
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}
			// API key is valid, add user to context and proceed.
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// This part needs a GetTokenByValue function in your database logic.
		tk, err := h.db.GetTokenByValue(token)
		if err != nil || tk.ExpiresAt.Before(time.Now()) {
			fmt.Println("Invalid session token:", token, err, tk)
			// If session is invalid, clear it and proceed without a user.
			h.Session.Remove(r.Context(), "token")
			ctx := context.WithValue(r.Context(), userContextKey, (*User)(nil))
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		user, err := h.db.GetUserByEmail(tk.Email) // Assumes GetUserByEmail exists
		if err != nil {
			http.Error(w, "Could not find user for session", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func (h *Handlers) GetTokenFromSession(r *http.Request) (string, error) {
	tk, ok := h.Session.Get(r.Context(), "token").(string)
	if !ok {
		return "", errors.New("error getting token from session")
	}
	return tk, nil
}

func (h *Handlers) AddTokenToSession(r *http.Request, w http.ResponseWriter, tk *Token) error {
	h.Session.Put(r.Context(), "token", tk.Token)
	return nil
}
func (h *Handlers) ClearSession(w http.ResponseWriter, r *http.Request) {
	h.Session.Remove(r.Context(), "token")
}

// handleLogin routes GET and POST requests for the /login page.
func (h *Handlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.showLoginPage(w, r, "")
	case http.MethodPost:
		h.processLogin(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// showLoginPage renders the login form.
func (h *Handlers) showLoginPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	h.templates.ExecuteTemplate(w, "login.html", LoginViewData{Error: errorMsg})
}

// processLogin handles the login form submission.
func (h *Handlers) processLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := h.db.GetUserByEmail(email)
	if err != nil {
		log.Printf("Error getting user by email: %v", err)
		h.showLoginPage(w, r, "Invalid email or password.")
		return
	}
	if user == nil {
		h.showLoginPage(w, r, "Invalid email or password.")
		return
	}

	ok, err := user.PasswordMatches(password)
	if err != nil {
		log.Printf("Error matching password: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		h.showLoginPage(w, r, "Invalid email or password.")
		return
	}

	tk, err := user.SessionToken.CreateToken(user.ID, 24*time.Hour)
	if err != nil {
		log.Printf("Error creating session token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	tk.Email = user.Email
	if err := h.db.SaveToken(tk); err != nil {
		log.Printf("Error saving session token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	err = h.AddTokenToSession(r, w, tk)
	if err != nil {
		log.Printf("Error adding token to session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/topics", http.StatusSeeOther)
}

// handleLogout clears the session cookie.
func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.Session.Remove(r.Context(), "token")
	http.Redirect(w, r, "/topics", http.StatusSeeOther)
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

	// Get user from context
	user, _ := r.Context().Value(userContextKey).(*User)

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
		User:        user, // Pass user to the template
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

	if len(parts) == 2 && parts[1] == "posts" {
		if r.Method == http.MethodPost {
			h.createPost(w, r, topicIDStr)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

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

	// Get user from context
	user, _ := r.Context().Value(userContextKey).(*User)

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
		User:  user, // Pass user to the template
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
	user, ok := r.Context().Value(userContextKey).(*User)
	if !ok || user == nil {
		http.Error(w, "You must be logged in to post", http.StatusUnauthorized)
		return
	}

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
		Author:  user.Handle, // Get author from logged-in user
		Body:    r.FormValue("body"),
	}

	if post.Body == "" {
		http.Error(w, "Body is a required field", http.StatusBadRequest)
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
