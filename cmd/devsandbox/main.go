// SPDX-License-Identifier: AGPL-3.0-only
// devsandbox runs the real application with loopback-only synthetic providers.
// It is a developer command, never part of the production image entrypoint.
package main

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/devmail"
	"mailspace.local/mailspace/internal/httpapi"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func privatePassword(path string, existingDB bool) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if existingDB {
			return "", errors.New("preview login file missing; restore its disposable state or choose a new sandbox directory")
		}
		raw = []byte(rand.Text())
		if err = os.WriteFile(path, raw, 0600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || len(raw) < 16 || len(raw) > 128 {
		return "", errors.New("invalid preview login file")
	}
	return string(raw), nil
}
func ensureUser(ctx context.Context, s *identity.Store, email, password string) (identity.User, error) {
	user, err := s.Authenticate(ctx, email, password)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, identity.ErrCredentials) {
		return identity.User{}, err
	}
	return s.CreateUser(ctx, email, password)
}
func run() error {
	dev := flag.Bool("dev", false, "reload local templates and assets during development")
	state := flag.String("data-dir", "data/dev-sandbox", "private, disposable preview state")
	addr := flag.String("addr", "127.0.0.1:8080", "numeric loopback HTTP address")
	imapAddr := flag.String("imap-addr", "127.0.0.1:21993", "numeric loopback synthetic IMAPS address")
	smtpAddr := flag.String("smtp-addr", "127.0.0.1:21465", "numeric loopback synthetic SMTPS address")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("sandbox HTTP must bind to numeric loopback")
	}
	absolute, err := filepath.Abs(*state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return err
	}
	dbPath := filepath.Join(absolute, "app.db")
	_, statErr := os.Stat(dbPath)
	existing := statErr == nil
	password, err := privatePassword(filepath.Join(absolute, "login-password"), existing)
	if err != nil {
		return err
	}
	memberPassword, err := privatePassword(filepath.Join(absolute, "member-password"), existing)
	if err != nil {
		return err
	}
	provider, err := devmail.Start(devmail.Options{StateDir: filepath.Join(absolute, "provider"), IMAPAddr: *imapAddr, SMTPAddr: *smtpAddr, Username: "demo@example.test"})
	if err != nil {
		return err
	}
	defer provider.Close()
	store, err := identity.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	owner, err := ensureUser(ctx, store, "demo@example.test", password)
	if err != nil {
		return err
	}
	member, err := ensureUser(ctx, store, "member@example.test", memberPassword)
	if err != nil {
		return err
	}
	if !existing {
		if err = store.UpdateProfile(ctx, owner.ID, "Jamie Parker", "Europe/Warsaw"); err != nil {
			return err
		}
		if err = store.UpdateProfile(ctx, member.ID, "Sam Rivera", "Europe/Warsaw"); err != nil {
			return err
		}
	}
	organizations, err := store.Workspaces(ctx, owner.ID)
	if err != nil {
		return err
	}
	if len(organizations) == 0 {
		w, err := store.CreateWorkspace(ctx, owner.ID, "Northstar studio")
		if err != nil {
			return err
		}
		if err = store.AddMember(ctx, owner.ID, w.ID, member.Email); err != nil {
			return err
		}
		if err = store.SwitchWorkspace(ctx, owner.ID, w.ID); err != nil {
			return err
		}
	}
	roots := x509.NewCertPool()
	pem, err := os.ReadFile(provider.CAFile)
	if err != nil {
		return err
	}
	if !roots.AppendCertsFromPEM(pem) {
		return errors.New("invalid fixture CA")
	}
	policy, err := mailnet.NewPolicy("", provider.IMAPAddr+","+provider.SMTPAddr)
	if err != nil {
		return err
	}
	connector := &mailnet.Connector{Policy: policy, Roots: roots}
	service, err := accounts.Open(store.Database(), filepath.Join(absolute, "credential-keys"), connector)
	if err != nil {
		return err
	}
	configured, err := store.Accounts(ctx, owner.ID)
	if err != nil {
		return err
	}
	if len(configured) == 0 {
		providerPassword, err := os.ReadFile(provider.PasswordFile)
		if err != nil {
			return err
		}
		login := mailnet.Login{Username: provider.Username, Password: string(providerPassword)}
		endpoint := func(address string) mailnet.Endpoint {
			host, port, _ := net.SplitHostPort(address)
			number, _ := strconv.Atoi(port)
			return mailnet.Endpoint{Host: host, Port: number, TLS: "tls"}
		}
		if _, err = service.Save(ctx, owner.ID, "", accounts.Input{Name: "Northstar mailbox", Address: provider.Username, SentMailbox: "Sent", IMAP: endpoint(provider.IMAPAddr), SMTP: endpoint(provider.SMTPAddr), IMAPLogin: login, SMTPLogin: login}); err != nil {
			return err
		}
	}
	syncer := mailsync.New(store.Database(), service, connector)
	outgoing := outbox.New(store.Database(), service, syncer, connector)
	if err = outgoing.Recover(ctx); err != nil {
		return err
	}
	configured, err = store.Accounts(ctx, owner.ID)
	if err != nil {
		return err
	}
	for _, a := range configured {
		if err = syncer.Sync(ctx, owner.ID, a.ID); err != nil {
			return fmt.Errorf("synthetic mailbox sync: %w", err)
		}
	}
	if !existing {
		if err = seedSpaces(ctx, store, service, syncer, outgoing, owner.ID); err != nil {
			return err
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); syncer.Run(ctx) }()
	defer func() { stop(); <-done }()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	origin := "http://" + listener.Addr().String()
	handler, err := httpapi.NewPersonal("dist", store, httpapi.AuthConfig{Origin: origin, InsecureLoopback: true, Accounts: service, Sync: syncer, Outbox: outgoing, MailEndpointsEnabled: true, Synthetic: true, Dev: *dev})
	if err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if server.Shutdown(deadline) != nil {
			server.Close()
		}
	}()
	log.Printf("Local synthetic sandbox: %s; login demo@example.test; password file %s", origin, filepath.Join(absolute, "login-password"))
	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		stop()
		return err
	}
	stop()
	<-shutdown
	return nil
}
