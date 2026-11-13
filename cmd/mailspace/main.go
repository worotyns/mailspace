// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"fmt"
	"log"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/httpapi"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "seed" {
		if err := mail.ValidateDemo(); err != nil {
			log.Fatal(err)
		}
		messages := 0
		for _, c := range mail.Demo() {
			messages += len(c.Messages)
		}
		fmt.Printf("Synthetic seed: %d conversations, %d messages, 4 channels. All addresses use .test.\n", len(mail.Demo()), messages)
		return
	}

	if len(os.Args) > 1 {
		if len(os.Args) == 3 && os.Args[1] == "credentials" && os.Args[2] == "rotate" {
			if err := rotateMailCredentials(); err != nil {
				log.Fatal(err)
			}
			return
		}
		if len(os.Args) == 4 && os.Args[1] == "user" && (os.Args[2] == "password" || os.Args[2] == "revoke-sessions") {
			if err := recoverUser(os.Args[3], os.Args[2] == "password"); err != nil {
				log.Fatal(err)
			}
			return
		}
		if len(os.Args) == 4 && os.Args[1] == "user" && os.Args[2] == "add" {
			if err := addUser(os.Args[3]); err != nil {
				log.Fatal(err)
			}
			return
		}
		log.Fatal("Usage: mailspace | mailspace seed | mailspace user add EMAIL | mailspace user password EMAIL | mailspace user revoke-sessions EMAIL | mailspace credentials rotate")
	}
	addr := os.Getenv("MAILSPACE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	dir := os.Getenv("MAILSPACE_WEB_DIR")
	if dir == "" {
		dir = "dist"
	}
	mode := os.Getenv("MAILSPACE_MODE")
	if mode == "" {
		mode = "personal"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var syncDone chan struct{}
	var handler http.Handler
	switch mode {
	case "personal":
		store, err := identity.Open(databasePath())
		if err != nil {
			log.Fatal(err)
		}
		defer store.Close()
		connector, policy, err := mailConnector()
		if err != nil {
			log.Fatal(err)
		}
		mailAccounts, err := accounts.Open(store.Database(), credentialKeysPath(), connector)
		if err != nil {
			log.Fatal(err)
		}
		syncer := mailsync.New(store.Database(), mailAccounts, connector)
		outgoing := outbox.New(store.Database(), mailAccounts, syncer, connector)
		if err = outgoing.Recover(ctx); err != nil {
			log.Fatal(err)
		}
		syncDone = make(chan struct{})
		go func() { defer close(syncDone); syncer.Run(ctx) }()
		defer func() { stop(); <-syncDone }()
		handler, err = httpapi.NewPersonal(dir, store, httpapi.AuthConfig{Outbox: outgoing, Sync: syncer, Accounts: mailAccounts, MailEndpointsEnabled: policy.HasEndpoints(), Origin: os.Getenv("MAILSPACE_ORIGIN"), InsecureLoopback: os.Getenv("MAILSPACE_INSECURE_HTTP") == "1", Dev: os.Getenv("MAILSPACE_DEV") == "1"})
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("MAILSPACE_MODE must be personal; use make dev-sandbox for synthetic mail")
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
		}
	}()
	log.Printf("Mailspace %s mode listening on %s; mail transport in personal mode; client OpenPGP/MIME alpha; see capabilities and compatibility limits", mode, addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}
