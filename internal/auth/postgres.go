package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/postgres"
)

type PostgresUsers struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPostgresUsers(pool *pgxpool.Pool, timeout time.Duration) *PostgresUsers {
	return &PostgresUsers{pool, timeout}
}
func (r *PostgresUsers) ByUsername(ctx context.Context, name string) (Account, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var a Account
	err := r.pool.QueryRow(ctx, `SELECT id,username,name,role,version,password_hash FROM auth.users WHERE username=$1`, name).Scan(&a.ID, &a.Username, &a.Name, &a.Role, &a.Version, &a.PasswordHash)
	return a, postgres.Error(ctx, err)
}
func (r *PostgresUsers) ByID(ctx context.Context, id string) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var u User
	err := r.pool.QueryRow(ctx, `SELECT id,username,name,role,version FROM auth.users WHERE id=$1`, id).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Version)
	return u, postgres.Error(ctx, err)
}
func (r *PostgresUsers) Create(ctx context.Context, a Account) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	a.Version = 1
	_, err := r.pool.Exec(ctx, `INSERT INTO auth.users(id,username,name,password_hash,role) VALUES($1,$2,$3,$4,$5)`, a.ID, a.Username, a.Name, a.PasswordHash, a.Role)
	return a.User, postgres.Error(ctx, err)
}
func (r *PostgresUsers) List(ctx context.Context) ([]User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT id,username,name,role,version FROM auth.users ORDER BY username`)
	if err != nil {
		return nil, postgres.Error(ctx, err)
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Version); err != nil {
			return nil, postgres.Error(ctx, err)
		}
		users = append(users, u)
	}
	return users, postgres.Error(ctx, rows.Err())
}
func (r *PostgresUsers) ChangeRole(ctx context.Context, actor, id, role string) (User, error) {
	if !ValidRole(role) {
		return User{}, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	defer postgres.Rollback(tx)
	// All role writers take this lock, preventing concurrent last-admin demotions.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73003002)`); err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	var actorRole string
	if err = tx.QueryRow(ctx, `SELECT role FROM auth.users WHERE id=$1`, actor).Scan(&actorRole); err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	if actorRole != Admin {
		return User{}, ErrForbidden
	}
	var u User
	if err = tx.QueryRow(ctx, `SELECT id,username,name,role,version FROM auth.users WHERE id=$1 FOR UPDATE`, id).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Version); err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	if u.Role == role {
		return u, nil
	}
	if u.Role == Admin && role != Admin {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE role='admin'`).Scan(&count); err != nil {
			return User{}, postgres.Error(ctx, err)
		}
		if count <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	old := u.Role
	if err = tx.QueryRow(ctx, `UPDATE auth.users SET role=$2,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1 RETURNING role,version`, id, role).Scan(&u.Role, &u.Version); err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth.role_changes(id,actor_id,user_id,old_role,new_role) VALUES($1::uuid,$2,$3,$4,$5)`, domain.NewUUID(), actor, id, old, role); err != nil {
		return User{}, postgres.Error(ctx, err)
	}
	return u, postgres.Error(ctx, tx.Commit(ctx))
}

// ImportAccounts is an explicit bootstrap operation, never part of login/startup.
// Reruns preserve passwords and any administrator-assigned roles already in SQL.
func (r *PostgresUsers) ImportAccounts(ctx context.Context, accounts []Account) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return postgres.Error(ctx, err)
	}
	defer postgres.Rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73003002)`); err != nil {
		return postgres.Error(ctx, err)
	}
	for _, a := range accounts {
		if domain.ValidateID("id", a.ID) != nil || !usernamePattern.MatchString(a.Username) || !ValidRole(a.Role) {
			return domain.ErrInvalid
		}
		if _, _, err := parseHash(a.PasswordHash); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO auth.users(id,username,name,password_hash,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, a.ID, a.Username, a.Name, a.PasswordHash, a.Role)
		if err != nil {
			return postgres.Error(ctx, err)
		}
		var name string
		if err = tx.QueryRow(ctx, `SELECT username FROM auth.users WHERE id=$1`, a.ID).Scan(&name); err != nil {
			return postgres.Error(ctx, err)
		}
		if name != a.Username {
			return fmt.Errorf("bootstrap ID belongs to another username")
		}
	}
	var admins int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE role='admin'`).Scan(&admins); err != nil {
		return postgres.Error(ctx, err)
	}
	if admins == 0 {
		return errors.New("bootstrap requires an admin account; run auth-setup -admin-only")
	}
	return postgres.Error(ctx, tx.Commit(ctx))
}
