// auth-setup creates ignored local demo credentials; never overwrites existing files.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"procureflow-ai/internal/auth"
)

func main() {
	adminOnly := flag.Bool("admin-only", false, "create initial admin files without changing existing demo files")
	flag.Parse()
	if err := run(*adminOnly); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(adminOnly bool) error {
	paths := []string{"secrets/admin-account.json", "secrets/admin-credentials.json"}
	if !adminOnly {
		paths = append(paths, "secrets/auth-users.json", "secrets/service-auth.key", "secrets/demo-credentials.json")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return fmt.Errorf("%s exists or cannot be inspected; preserving it", path)
		}
	}
	if err := os.MkdirAll("secrets", 0700); err != nil {
		return err
	}
	if err := os.Chmod("secrets", 0700); err != nil {
		return err
	}
	accounts, credentials, err := generate([]auth.User{{ID: "admin-demo", Username: "admin.demo", Name: "Demo Administrator", Role: auth.Admin}})
	if err != nil {
		return err
	}
	data := map[string][]byte{"secrets/admin-account.json": accounts, "secrets/admin-credentials.json": credentials}
	if !adminOnly {
		accounts, credentials, err = generate([]auth.User{{ID: "emp-101", Username: "employee.demo", Name: "Demo Employee", Role: auth.Employee}, {ID: "emp-102", Username: "employee.two", Name: "Second Employee", Role: auth.Employee}, {ID: "manager-demo", Username: "manager.demo", Name: "Demo Manager", Role: auth.Manager}})
		if err != nil {
			return err
		}
		data["secrets/auth-users.json"] = accounts
		data["secrets/demo-credentials.json"] = credentials
		data["secrets/service-auth.key"] = []byte(auth.RandomSecret() + "\n")
	}
	for _, path := range paths {
		mode := os.FileMode(0644)
		if strings.Contains(path, "credentials") {
			mode = 0600
		}
		file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = file.Write(data[path])
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Println("Account files created. Read secrets/admin-credentials.json locally for the admin login. Existing files were not overwritten.")
	return nil
}
func generate(users []auth.User) ([]byte, []byte, error) {
	var accounts []auth.Account
	var credentials []map[string]string
	for _, user := range users {
		password := auth.RandomSecret()
		hash, err := auth.HashPassword(password)
		if err != nil {
			return nil, nil, err
		}
		accounts = append(accounts, auth.Account{User: user, PasswordHash: hash})
		credentials = append(credentials, map[string]string{"username": user.Username, "password": password, "id": user.ID, "role": user.Role})
	}
	encoded, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	logins, err := json.MarshalIndent(credentials, "", "  ")
	return encoded, logins, err
}
