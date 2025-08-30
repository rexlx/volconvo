package forum

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Token struct {
	Handle    string
	ID        string
	Email     string
	UserID    string
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Hash      []byte
}

func (t *Token) MarshalBinary() ([]byte, error) {
	return json.Marshal(t)
}

func (t *Token) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, t)
}

func (t *Token) CreateToken(userID string, ttl time.Duration) (*Token, error) {
	tk := &Token{
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
	}
	hotSauce := make([]byte, 64)
	_, err := io.ReadFull(rand.Reader, hotSauce)
	if err != nil {
		return nil, err
	}
	tk.Token = uuid.New().String()
	hash := sha256.Sum256([]byte(tk.Token))
	tk.Hash = hash[:]
	tk.ID = uuid.New().String()
	// fmt.Printf("Generated token: %+v\n", tk)
	return tk, nil
}

func NewUser(email string, admin bool) (*User, error) {
	id := uuid.New().String()
	key, err := generateAPIKey()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	notifications := make([]Notification, 0)
	return &User{
		Notifications: notifications,
		ID:            id,
		Email:         email,
		Key:           key,
		Created:       now,
		Updated:       now,
		Admin:         admin,
	}, nil
}

type User struct {
	ID            string         `json:"id"`
	Email         string         `json:"email"`
	Key           string         `json:"key"`
	Hash          []byte         `json:"hash"`
	Password      string         `json:"password"`
	Created       time.Time      `json:"created"`
	Updated       time.Time      `json:"updated"`
	Handle        string         `json:"handle"`
	Admin         bool           `json:"admin"`
	SessionToken  *Token         `json:"session_token"`
	Notifications []Notification `json:"notifications"`
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	u.Hash = hash
	u.Password = string(hash)
	return nil
}

func (u *User) MarshalBinary() ([]byte, error) {
	return json.Marshal(u)
}

func (u *User) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, u)
}

func (u *User) PasswordMatches(input string) (bool, error) {
	err := bcrypt.CompareHashAndPassword(u.Hash, []byte(input))
	if err != nil {
		switch {
		case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
			//invalid password
			return false, nil
		default:
			//unknown error
			return false, err
		}
	}

	return true, nil
}

func (u *User) Sanitize() {
	u.Hash = nil
	u.Password = ""
}

func generateAPIKey() (string, error) {
	thatThing := make([]byte, 32)
	_, err := rand.Read(thatThing)
	if err != nil {
		return "", err
	}
	hashed := sha256.Sum256(thatThing)
	key := base64.StdEncoding.EncodeToString(hashed[:])
	return key, nil
}

type Notification struct {
	From      string    `json:"from"`
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	ReadAt    time.Time `json:"read_at"`
	Link      string    `json:"link"`
}
