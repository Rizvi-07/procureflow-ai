package postgres_test

import (
	"errors"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/postgres"
	"procureflow-ai/migrations"
	"sync"
	"testing"
	"time"
)

func TestPostgresUsersSignupRolesAndPersistence(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	repo := auth.NewPostgresUsers(db.pool, 3*time.Second)
	hash, err := auth.HashPassword("fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	admin := auth.Account{User: auth.User{ID: "admin-demo", Username: "admin.demo", Name: "Admin", Role: auth.Admin}, PasswordHash: hash}
	if err := repo.ImportAccounts(ctx, []auth.Account{admin}); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewPersistentSessions(repo, false)
	user, err := sessions.Register(ctx, " Employee.New ", "Fictional New Employee", "fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != auth.Employee {
		t.Fatal("signup granted privileges")
	}
	if _, err := sessions.Register(ctx, "EMPLOYEE.NEW", "Duplicate", "fictional-test-password"); !errors.Is(err, domain.ErrExists) {
		t.Fatal("duplicate username", err)
	}
	if _, err := repo.ChangeRole(ctx, user.ID, user.ID, auth.Admin); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("nonadmin changed role", err)
	}
	if _, err := repo.ChangeRole(ctx, admin.ID, admin.ID, auth.Employee); !errors.Is(err, auth.ErrLastAdmin) {
		t.Fatal("last admin guard", err)
	}
	token, _, _ := sessions.Create(user)
	changed, err := repo.ChangeRole(ctx, admin.ID, user.ID, auth.Manager)
	if err != nil || changed.Version != user.Version+1 {
		t.Fatal("role version", err)
	}
	if _, ok, err := sessions.Resolve(ctx, token); ok || err != nil {
		t.Fatal("stale session", err)
	}
	// Reimporting stale bootstrap files must not reset a changed role/password.
	imported := auth.Account{User: user, PasswordHash: hash}
	if err := repo.ImportAccounts(ctx, []auth.Account{admin, imported}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ChangeRole(ctx, admin.ID, user.ID, auth.Manager); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM auth.role_changes WHERE user_id=$1 AND actor_id=$2 AND old_role='employee' AND new_role='manager'`, user.ID, admin.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("atomic audit / repeated update", count, err)
	}
	if err := migrations.Apply(ctx, db.pool); err != nil {
		t.Fatal(err)
	}
	db.pool.Close()
	db.pool, err = postgres.Open(ctx, db.url, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	repo = auth.NewPostgresUsers(db.pool, 3*time.Second)
	sessions = auth.NewPersistentSessions(repo, false)
	saved, ok, err := sessions.AuthenticateContext(ctx, "employee.new", "fictional-test-password")
	if err != nil || !ok || saved.ID != user.ID || saved.Role != auth.Manager {
		t.Fatal("identity did not persist", err)
	}
	users, err := repo.List(ctx)
	if err != nil || len(users) != 2 {
		t.Fatal("list persistence", err)
	}
	for _, query := range []string{`UPDATE auth.users SET role='owner' WHERE id='admin-demo'`, `UPDATE auth.users SET username='MixedCase' WHERE id='admin-demo'`, `UPDATE auth.users SET version=0 WHERE id='admin-demo'`} {
		if _, err := db.pool.Exec(ctx, query); err == nil {
			t.Fatal("SQL constraint accepted invalid input")
		}
	}
}
func TestPostgresConcurrentAdminDemotions(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	repo := auth.NewPostgresUsers(db.pool, 3*time.Second)
	hash, err := auth.HashPassword("fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	accounts := []auth.Account{{User: auth.User{ID: "admin-one", Username: "admin.one", Name: "One", Role: auth.Admin}, PasswordHash: hash}, {User: auth.User{ID: "admin-two", Username: "admin.two", Name: "Two", Role: auth.Admin}, PasswordHash: hash}}
	if err := repo.ImportAccounts(ctx, accounts); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, account := range accounts {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := repo.ChangeRole(ctx, id, id, auth.Employee)
			results <- err
		}(account.ID)
	}
	wg.Wait()
	close(results)
	success := 0
	blocked := 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, auth.ErrLastAdmin) {
			blocked++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || blocked != 1 {
		t.Fatalf("demotions=%d blocked=%d", success, blocked)
	}
	var admins int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE role='admin'`).Scan(&admins); err != nil || admins != 1 {
		t.Fatal("last admin race", admins, err)
	}
}
