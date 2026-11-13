// SPDX-License-Identifier: AGPL-3.0-only
package mailnet_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/emersion/go-imap/v2/imapclient"
	"mailspace.local/mailspace/internal/devmail"
	"mailspace.local/mailspace/internal/mailnet"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPagedReadLimitsAndCancellation(t *testing.T) {
	server, err := devmail.Start(devmail.Options{StateDir: t.TempDir(), IMAPAddr: "127.0.0.1:0", SMTPAddr: "127.0.0.1:0", Username: "demo@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	password, err := os.ReadFile(server.PasswordFile)
	if err != nil {
		t.Fatal(err)
	}
	pem, err := os.ReadFile(server.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem)
	policy, err := mailnet.NewPolicy("", server.IMAPAddr)
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(server.IMAPAddr)
	p, _ := strconv.Atoi(port)
	ep := mailnet.Endpoint{Host: host, Port: p, TLS: "tls"}
	login := mailnet.Login{Username: server.Username, Password: string(password)}
	client, err := imapclient.DialTLS(server.IMAPAddr, &imapclient.Options{TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.Login(login.Username, login.Password).Wait(); err != nil {
		t.Fatal(err)
	}
	appendMail := func(raw string) uint32 {
		t.Helper()
		cmd := client.Append("INBOX", int64(len(raw)), nil)
		if _, err = cmd.Write([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		if err = cmd.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := cmd.Wait()
		if err != nil {
			t.Fatal(err)
		}
		return uint32(data.UID)
	}
	for i := 0; i < 40; i++ {
		appendMail("From: synthetic@example.test\r\nSubject: Page test\r\n\r\nLocal test")
	}
	largeUID := appendMail("Subject: Large\r\n\r\n" + strings.Repeat("x", mailnet.MaxMIMEBytes+1))
	connector := &mailnet.Connector{Policy: policy, Roots: roots, Timeout: 2 * time.Second}
	reader, err := connector.OpenRead(context.Background(), ep, login)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot("INBOX")
	if err != nil || len(snapshot.Messages) != 54 {
		t.Fatal("paged fetch failed", len(snapshot.Messages), err)
	}
	if _, err = reader.MIME("INBOX", snapshot.UIDValidity, largeUID); !errors.Is(err, mailnet.ErrMailLimit) {
		t.Fatal("MIME literal budget ignored", err)
	}
	reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	reader, err = connector.OpenRead(ctx, ep, login)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	started := time.Now()
	if _, err = reader.Snapshot("INBOX"); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	reader.Close()
	if time.Since(started) > time.Second {
		t.Fatal("cancel did not close socket promptly")
	}
}
