// SPDX-License-Identifier: AGPL-3.0-only
package devmail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"mailspace.local/mailspace/internal/mailnet"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestSeededTLSFixtureAndUIDValidityRestart(t *testing.T) {
	dir := t.TempDir()
	options := Options{StateDir: dir, IMAPAddr: "127.0.0.1:0", SMTPAddr: "127.0.0.1:0", Username: "demo@example.test"}
	check := func(server *Server) uint32 {
		t.Helper()
		password, err := os.ReadFile(server.PasswordFile)
		if err != nil {
			t.Fatal(err)
		}
		pem, err := os.ReadFile(server.CAFile)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("CA missing")
		}
		policy, err := mailnet.NewPolicy("", server.IMAPAddr+","+server.SMTPAddr)
		if err != nil {
			t.Fatal(err)
		}
		connector := mailnet.Connector{Policy: policy, Roots: roots, Timeout: 2 * time.Second}
		ep := func(address string) mailnet.Endpoint {
			host, port, _ := net.SplitHostPort(address)
			number, _ := strconv.Atoi(port)
			return mailnet.Endpoint{Host: host, Port: number, TLS: "tls"}
		}
		login := mailnet.Login{Username: server.Username, Password: string(password)}
		if err = connector.CheckIMAP(context.Background(), ep(server.IMAPAddr), login); err != nil {
			t.Fatal(err)
		}
		if err = connector.CheckSMTP(context.Background(), ep(server.SMTPAddr), login); err != nil {
			t.Fatal(err)
		}
		client, err := imapclient.DialTLS(server.IMAPAddr, &imapclient.Options{TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err = client.Login(server.Username, string(password)).Wait(); err != nil {
			t.Fatal(err)
		}
		selected, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			t.Fatal(err)
		}
		if selected.NumMessages != 13 {
			t.Fatalf("unexpected inbox seed: %d", selected.NumMessages)
		}
		return selected.UIDValidity
	}
	first, err := Start(options)
	if err != nil {
		t.Fatal(err)
	}
	before := check(first)
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Start(options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after := check(second)
	if after <= before {
		t.Fatal("fixture reused UIDVALIDITY across memory resets")
	}
}
