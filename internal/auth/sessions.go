package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"procureflow-ai/internal/domain"
)

const CookieName = "procureflow_session"
const Employee = "employee"
const Manager = "manager"

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Version  int64  `json:"-"`
}
type Account struct {
	User
	PasswordHash string `json:"password_hash"`
}
type Session struct {
	User    User
	CSRF    string
	Expires time.Time
}
type attempt struct {
	Count   int
	Expires time.Time
}
type Sessions struct {
	mu            sync.Mutex
	Users         Users
	sessions      map[[32]byte]Session
	attempts      map[string]attempt
	passwordSlots chan struct{}
	Now           func() time.Time
	TTL           time.Duration
	Secure        bool
}

// LoadAccounts validates the trusted local account file once at gateway startup.
func LoadAccounts(path string) ([]Account, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open AUTH_USERS_FILE; run go run ./cmd/auth-setup first")
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() > 65536 {
		return nil, fmt.Errorf("accounts file exceeds size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	var accounts []Account
	if err = decoder.Decode(&accounts); err != nil || len(accounts) < 1 || len(accounts) > 20 {
		return nil, fmt.Errorf("invalid local accounts file")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid accounts file size or trailing data")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, a := range accounts {
		if domain.ValidateID("user ID", a.ID) != nil || len(a.Username) < 3 || len(a.Username) > 80 || a.Username != strings.ToLower(strings.TrimSpace(a.Username)) || a.Name == "" || len(a.Name) > 120 || !ValidRole(a.Role) || ids[a.ID] || names[a.Username] {
			return nil, fmt.Errorf("invalid or duplicate local account")
		}
		if _, _, err = parseHash(a.PasswordHash); err != nil {
			return nil, err
		}
		ids[a.ID] = true
		names[a.Username] = true
	}
	return accounts, nil
}

// NewSessions keeps only session token digests; a process restart signs everyone out.
func NewSessions(accounts []Account, secure bool) *Sessions {
	return NewPersistentSessions(NewMemoryUsers(accounts), secure)
}
func NewPersistentSessions(users Users, secure bool) *Sessions {
	return &Sessions{Users: users, sessions: map[[32]byte]Session{}, attempts: map[string]attempt{}, passwordSlots: make(chan struct{}, 4), Now: time.Now, TTL: 8 * time.Hour, Secure: secure}
}

// AllowLogin bounds both individual account guessing and attempts from one host.
func (s *Sessions) AllowLogin(ip, username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for k, a := range s.attempts {
		if !now.Before(a.Expires) {
			delete(s.attempts, k)
		}
	}
	for _, entry := range []struct {
		key string
		max int
	}{{"ip:" + ip, 20}, {"name:" + username, 10}} {
		a := s.attempts[entry.key]
		if a.Count >= entry.max || (a.Count == 0 && len(s.attempts) >= 1024) {
			return false
		}
	}
	for _, k := range []string{"ip:" + ip, "name:" + username} {
		a := s.attempts[k]
		if a.Count == 0 {
			a.Expires = now.Add(time.Minute)
		}
		a.Count++
		s.attempts[k] = a
	}
	return true
}

// Authenticate bounds expensive hashes and performs a dummy hash for unknown users.
var dummyHash = sync.OnceValue(func() string {
	hash, err := HashPassword(RandomSecret())
	if err != nil {
		panic("cannot initialize password verifier")
	}
	return hash
})

func (s *Sessions) Authenticate(username, password string) (User, bool) {
	user, ok, _ := s.AuthenticateContext(context.Background(), username, password)
	return user, ok
}
func (s *Sessions) AuthenticateContext(ctx context.Context, username, password string) (User, bool, error) {
	if err := ctx.Err(); err != nil {
		return User{}, false, err
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		return User{}, false, ErrBusy
	}
	a, err := s.Users.ByUsername(ctx, NormalizeUsername(username))
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return User{}, false, err
	}
	encoded := a.PasswordHash
	if errors.Is(err, domain.ErrNotFound) {
		encoded = dummyHash()
	}
	valid := VerifyPassword(encoded, password)
	if err := ctx.Err(); err != nil {
		return User{}, false, err
	}
	return a.User, err == nil && valid, nil
}

// Register always creates employees; neither IDs nor privileged roles come from JSON.
func (s *Sessions) Register(ctx context.Context, username, name, password string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	username = NormalizeUsername(username)
	name = strings.TrimSpace(name)
	if err := ValidateSignup(username, name, password); err != nil {
		return User{}, err
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		return User{}, ErrBusy
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.Users.Create(ctx, Account{User: User{ID: domain.NewUUID(), Username: username, Name: name, Role: Employee}, PasswordHash: hash})
}

// Resolve checks SQL on every HTTP request, so role changes invalidate old sessions.
func (s *Sessions) Resolve(ctx context.Context, token string) (Session, bool, error) {
	session, ok := s.Lookup(token)
	if !ok {
		return Session{}, false, nil
	}
	user, err := s.Users.ByID(ctx, session.User.ID)
	if errors.Is(err, domain.ErrNotFound) {
		s.Revoke(token)
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	if user.Version != session.User.Version || user.Role != session.User.Role {
		s.Revoke(token)
		return Session{}, false, nil
	}
	session.User = user
	return session, true, nil
}
func (s *Sessions) Create(user User) (string, Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for k, v := range s.sessions {
		if !now.Before(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 4096 {
		return "", Session{}, fmt.Errorf("session capacity reached")
	}
	token := RandomSecret()
	session := Session{user, RandomSecret(), now.Add(s.TTL)}
	s.sessions[sha256.Sum256([]byte(token))] = session
	return token, session, nil
}
func (s *Sessions) Lookup(token string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	v, ok := s.sessions[key]
	if !ok || !s.Now().Before(v.Expires) {
		delete(s.sessions, key)
		return Session{}, false
	}
	return v, true
}
func (s *Sessions) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
}
