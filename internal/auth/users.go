package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"procureflow-ai/internal/domain"
)

const Admin = "admin"

var (
	ErrForbidden    = errors.New("admin role required")
	ErrLastAdmin    = errors.New("the last admin cannot be demoted")
	ErrBusy         = errors.New("password processing is busy; try again")
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,79}$`)
)

// Users isolates identity persistence from sessions and HTTP transport.
type Users interface {
	ByUsername(context.Context, string) (Account, error)
	ByID(context.Context, string) (User, error)
	Create(context.Context, Account) (User, error)
	List(context.Context) ([]User, error)
	ChangeRole(context.Context, string, string, string) (User, error)
}

func CanReview(role string) bool           { return role == Manager || role == Admin }
func ValidRole(role string) bool           { return role == Employee || role == Manager || role == Admin }
func NormalizeUsername(name string) string { return strings.ToLower(strings.TrimSpace(name)) }
func ValidateSignup(username, name, password string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("%w: username must contain 3-80 lowercase letters, digits, dots, underscores or hyphens, starting alphanumeric", domain.ErrInvalid)
	}
	if !utf8.ValidString(name) || strings.ContainsRune(name, 0) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 120 {
		return fmt.Errorf("%w: name must contain 1-120 characters", domain.ErrInvalid)
	}
	if len(password) < 12 || len(password) > 128 {
		return fmt.Errorf("%w: password must contain 12-128 bytes", domain.ErrInvalid)
	}
	return nil
}

// MemoryUsers is a concurrent fake used by unit tests. Runtime uses PostgreSQL.
type MemoryUsers struct {
	mu       sync.Mutex
	accounts map[string]Account
}

func NewMemoryUsers(accounts []Account) *MemoryUsers {
	r := &MemoryUsers{accounts: map[string]Account{}}
	for _, a := range accounts {
		r.accounts[a.ID] = a
	}
	return r
}
func (r *MemoryUsers) ByUsername(ctx context.Context, name string) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts {
		if a.Username == name {
			return a, nil
		}
	}
	return Account{}, domain.ErrNotFound
}
func (r *MemoryUsers) ByID(ctx context.Context, id string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok {
		return User{}, domain.ErrNotFound
	}
	return a.User, nil
}
func (r *MemoryUsers) Create(ctx context.Context, a Account) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if existing.Username == a.Username || existing.ID == a.ID {
			return User{}, domain.ErrExists
		}
	}
	a.Version = 1
	r.accounts[a.ID] = a
	return a.User, nil
}
func (r *MemoryUsers) List(ctx context.Context) ([]User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	users := make([]User, 0, len(r.accounts))
	for _, a := range r.accounts {
		users = append(users, a.User)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users, nil
}
func (r *MemoryUsers) ChangeRole(ctx context.Context, actor, id, role string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if !ValidRole(role) {
		return User{}, domain.ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.accounts[actor].Role != Admin {
		return User{}, ErrForbidden
	}
	a, ok := r.accounts[id]
	if !ok {
		return User{}, domain.ErrNotFound
	}
	if a.Role == role {
		return a.User, nil
	}
	if a.Role == Admin && role != Admin {
		count := 0
		for _, u := range r.accounts {
			if u.Role == Admin {
				count++
			}
		}
		if count <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	a.Role = role
	a.Version++
	r.accounts[id] = a
	return a.User, nil
}
