// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/term"
	"io"
	"mailspace.local/mailspace/internal/identity"
	"os"
)

func databasePath() string {
	if path := os.Getenv("MAILSPACE_DB"); path != "" {
		return path
	}
	return "data/mailspace.db"
}
func addUser(email string) error {
	if _, err := identity.NormalizeEmail(email); err != nil {
		return err
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	defer clear(password)
	store, err := identity.Open(databasePath())
	if err != nil {
		return err
	}
	defer store.Close()
	user, err := store.CreateUser(context.Background(), email, string(password))
	if err != nil {
		return err
	}
	fmt.Printf("Created Mailspace user %s. No mail account was connected.\n", user.Email)
	return nil
}

func readPassword() ([]byte, error) {
	var password []byte
	var err error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password (at least 12 characters): ")
		password, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	} else {
		password, err = io.ReadAll(io.LimitReader(os.Stdin, 1026))
		password = bytes.TrimSuffix(password, []byte("\n"))
		password = bytes.TrimSuffix(password, []byte("\r"))
	}
	if err != nil {
		return nil, err
	}

	return password, nil
}

func recoverUser(email string, reset bool) error {
	if _, err := identity.NormalizeEmail(email); err != nil {
		return err
	}
	var password []byte
	var err error
	if reset {
		password, err = readPassword()
		if err != nil {
			return err
		}
		defer clear(password)
	}
	store, err := identity.Open(databasePath())
	if err != nil {
		return err
	}
	defer store.Close()
	if reset {
		err = store.ResetPassword(context.Background(), email, string(password))
	} else {
		err = store.RevokeUserSessions(context.Background(), email)
	}
	if err == nil {
		fmt.Println("User sessions revoked. Provider credentials and local OpenPGP keys are unchanged.")
	}
	return err
}
