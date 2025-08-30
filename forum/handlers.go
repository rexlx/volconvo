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

// Corrected context key to store the user object, not the token.
const userContextKey = contextKey("user")

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
	User        *User
}

// TopicViewData is the data structure for the single topic page.
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

// NotificationsViewData is for the notifications page.
type NotificationsViewData struct {
	User          *User
	Notifications []Notification
}

type Handlers struct {
	NotifCh   chan Notification
	Session   *scs.SessionManager `json:"-"`
	db        *Database
	templates *template.Template
}

func NewHandlers(db *Database) (*Handlers, error) {
	ntfCh := make(chan Notification, 100)
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
		NotifCh:   ntfCh,
		Session:   sessionMgr,
		db:        db,
		templates: tpl,
	}
	return hndlr, nil
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("/api/user/create", h.addUserHandler)
	mux.HandleFunc("/api/notifications/delete", h.deleteNotificationHandler) // New route

	// Auth routes
	mux.HandleFunc("/login", h.handleLogin)
	mux.HandleFunc("/logout", h.handleLogout)
	mux.HandleFunc("/notifications", h.listNotificationsHandler) // New route

	// Content routes with auth middleware
	mux.Handle("/topics", h.ValidateSessionToken(http.HandlerFunc(h.handleTopics)))
	mux.Handle("/topics/", h.ValidateSessionToken(http.HandlerFunc(h.showTopic)))
}

// listNotificationsHandler displays the user's notifications.
func (h *Handlers) listNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	tkn, err := h.GetTokenFromSession(r)

	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	tk, err := h.db.GetTokenByValue(tkn)
	if err != nil || tk.ExpiresAt.Before(time.Now()) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	user, err := h.db.GetUserByEmail(tk.Email)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Mark notifications as read when the page is viewed.
	var changed bool
	for i := range user.Notifications {
		if user.Notifications[i].ReadAt.IsZero() {
			user.Notifications[i].ReadAt = time.Now()
			changed = true
		}
	}

	if changed {
		if err := h.db.SaveUser(user); err != nil {
			log.Printf("Error marking notifications as read: %v", err)
			// Non-critical error, so we still render the page.
		}
	}

	data := NotificationsViewData{
		User:          user,
		Notifications: user.Notifications,
	}
	err = h.templates.ExecuteTemplate(w, "notifications.html", data)
	if err != nil {
		log.Printf("Error executing notifications template: %v", err)
	}
}

// deleteNotificationHandler removes a notification for the logged-in user.
func (h *Handlers) deleteNotificationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := r.Context().Value(userContextKey).(*User)
	if !ok || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	notificationID := r.FormValue("id")
	if notificationID == "" {
		http.Error(w, "Missing notification ID", http.StatusBadRequest)
		return
	}

	var found bool
	var updatedNotifications []Notification
	for _, n := range user.Notifications {
		if n.ID == notificationID {
			found = true
		} else {
			updatedNotifications = append(updatedNotifications, n)
		}
	}

	if !found {
		http.NotFound(w, r)
		return
	}

	user.Notifications = updatedNotifications
	if err := h.db.SaveUser(user); err != nil {
		log.Printf("Error deleting notification: %v", err)
		http.Error(w, "Failed to delete notification", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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

	existingUser, _ := h.db.GetUserByEmail(req.Email)
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

	user.Sanitize()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// ValidateSessionToken checks for a valid session and adds the user to the request context.
func (h *Handlers) ValidateSessionToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := h.GetTokenFromSession(r)
		if err != nil {
			// No session token, check for API key
			apiKey := r.Header.Get("Authorization")
			parts := strings.Split(apiKey, ":")
			if apiKey == "" || len(parts) != 2 {
				ctx := context.WithValue(r.Context(), userContextKey, (*User)(nil))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			user, err := h.db.GetUserByEmail(parts[0])
			if err != nil || user == nil || user.Key != parts[1] {
				http.Error(w, "Invalid API key", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

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

func (h *Handlers) showLoginPage(w http.ResponseWriter, r *http.Request, errorMsg string) {
	h.templates.ExecuteTemplate(w, "login.html", LoginViewData{Error: errorMsg})
}

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

func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.Session.Remove(r.Context(), "token")
	http.Redirect(w, r, "/topics", http.StatusSeeOther)
}

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

func (h *Handlers) listTopics(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	searchQuery := r.URL.Query().Get("q")

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
		User:        user,
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

func (h *Handlers) showTopic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/topics/")
	parts := strings.Split(path, "/")
	topicIDStr := parts[0]

	if len(parts) == 2 && parts[1] == "posts" {
		fmt.Println("Creating post for topic:", topicIDStr, parts)
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

	user, _ := r.Context().Value(userContextKey).(*User)

	topic, err := h.db.GetTopic(topicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	posts, err := h.db.GetPostsByTopic(topicID, page, PageSize)
	if err != nil {
		http.Error(w, "Failed to retrieve posts", http.StatusInternalServerError)
		return
	}

	totalPosts, err := h.db.CountPostsByTopic(topicID)
	if err != nil {
		http.Error(w, "Failed to retrieve posts", http.StatusInternalServerError)
		return
	}

	totalPages := (totalPosts + PageSize - 1) / PageSize
	data := TopicViewData{
		Topic: *topic,
		Posts: posts,
		User:  user,
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
	// user, ok := r.Context().Value(userContextKey).(*User)
	// if !ok || user == nil {
	// 	http.Error(w, "You must be logged in to post", http.StatusUnauthorized)
	// 	return
	// }
	token, err := h.GetTokenFromSession(r)
	if err != nil {
		http.Error(w, "Failed to retrieve token from session", http.StatusInternalServerError)
		return
	}
	tk, err := h.db.GetTokenByValue(token)
	if err != nil {
		http.Error(w, "Failed to retrieve token from database", http.StatusInternalServerError)
		return
	}
	user, err := h.db.GetUserByEmail(tk.Email)
	if err != nil {
		http.Error(w, "Failed to retrieve user from database", http.StatusInternalServerError)
		return
	}

	// topicID, err := uuid.Parse(topicIDStr)
	// if err != nil {
	// 	http.Error(w, "Invalid topic ID", http.StatusBadRequest)
	// 	return
	// }
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	userID := r.FormValue("user_id")
	parentPostID := r.FormValue("parent_post_id")
	_post, err := strconv.Atoi(parentPostID)
	if err != nil {
		http.Error(w, "Invalid parent post ID", http.StatusBadRequest)
		return
	}

	postId, err := h.db.GetPost(int64(_post))
	if err != nil {
		http.Error(w, "Failed to retrieve post from database", http.StatusInternalServerError)
		return
	}
	post := Post{
		TopicID:  topicIDStr,
		Author:   user.Handle,
		Body:     r.FormValue("body"),
		AuthorID: user.ID,
	}
	// TODO: nothing is listening yet!
	h.NotifCh <- Notification{
		From:      userID,
		UserID:    postId.AuthorID,
		CreatedAt: time.Now(),
		Message:   fmt.Sprintf("New post created in topic %s, (%s)", topicIDStr, parentPostID),
		Link:      "/topics/" + topicIDStr,
		ID:        uuid.New().String(),
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

func (h *Handlers) createTopic(w http.ResponseWriter, r *http.Request) {
	var topic Topic
	if err := json.NewDecoder(r.Body).Decode(&topic); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if topic.ID == "" || topic.Title == "" {
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

func (h *Handlers) StartNotificationListener(rate time.Duration) {
	ticker := time.NewTicker(rate)
	defer ticker.Stop()

	for {
		select {
		case notif := <-h.NotifCh:
			if notif.UserID != "" {
				user, err := h.db.GetUserByID(notif.UserID)
				if err != nil {
					fmt.Printf("Error retrieving user %s: %v\n", notif.UserID, err)
					continue
				}
				user.Notifications = append(user.Notifications, notif)
				go h.db.SaveUser(user)
				// Send the notification to the user
				fmt.Printf("Sending notification to user %s: %s\n", user.Email, notif.Message)
			}
		case <-ticker.C:
			// Periodically check for new notifications
			fmt.Println("some sort of maintenance task")
		}
	}
}
