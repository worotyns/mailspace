// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"log"
	"mailspace.local/mailspace/internal/devmail"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	dir := flag.String("data-dir", "data/devmail", "private fixture state directory")
	imap := flag.String("imap-addr", "127.0.0.1:1993", "loopback implicit-TLS IMAP address")
	smtp := flag.String("smtp-addr", "127.0.0.1:1465", "loopback implicit-TLS SMTP address")
	username := flag.String("username", "demo@example.test", "synthetic .test user")
	flag.Parse()
	server, err := devmail.Start(devmail.Options{StateDir: *dir, IMAPAddr: *imap, SMTPAddr: *smtp, Username: *username})
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	log.Printf("Synthetic mail fixture ready: IMAPS %s; SMTPS %s; user %s; password file %s; CA file %s", server.IMAPAddr, server.SMTPAddr, server.Username, server.PasswordFile, server.CAFile)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
