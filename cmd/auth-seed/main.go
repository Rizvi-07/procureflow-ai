// auth-seed explicitly imports legacy demo accounts and the initial admin into SQL.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/postgres"
	"time"
)

func main() {
	usersPath := flag.String("users-file", "secrets/auth-users.json", "legacy demo accounts (hashes only)")
	adminPath := flag.String("admin-file", "secrets/admin-account.json", "initial admin account (hash only)")
	flag.Parse()
	if err := run(*usersPath, *adminPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(usersPath, adminPath string) error {
	url := os.Getenv("AUTH_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("AUTH_DATABASE_URL is required")
	}
	accounts, err := auth.LoadAccounts(usersPath)
	if err != nil {
		return err
	}
	admin, err := auth.LoadAccounts(adminPath)
	if err != nil {
		return fmt.Errorf("load initial admin; run auth-setup -admin-only")
	}
	if len(admin) != 1 || admin[0].Role != auth.Admin {
		return fmt.Errorf("admin file must contain exactly one admin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, url, 5*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := auth.NewPostgresUsers(pool, 10*time.Second).ImportAccounts(ctx, append(accounts, admin...)); err != nil {
		return fmt.Errorf("account bootstrap failed: %w", err)
	}
	fmt.Println("Identity bootstrap complete; existing account passwords and roles preserved.")
	return nil
}
