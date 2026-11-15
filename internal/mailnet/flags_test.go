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
	"testing"
	"time"
)

// SetSeen is the only provider flag mutation. It must apply exactly one flag to
// exactly one UID, and fail closed on a stale UIDVALIDITY or unknown message.
func TestSetSeenAppliesOnlyTheRequestedFlag(t *testing.T) {
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
	raw := "From: synthetic@example.test\r\nSubject: Flag test\r\n\r\nLocal test"
	cmd := client.Append("INBOX", int64(len(raw)), nil)
	if _, err = cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Close(); err != nil {
		t.Fatal(err)
	}
	appended, err := cmd.Wait()
	if err != nil {
		t.Fatal(err)
	}
	uid := uint32(appended.UID)
	connector := &mailnet.Connector{Policy: policy, Roots: roots, Timeout: 3 * time.Second}
	reader, err := connector.OpenRead(context.Background(), ep, login)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot("INBOX")
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	flags := func() []string {
		t.Helper()
		session, err := connector.OpenRead(context.Background(), ep, login)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		box, err := session.Snapshot("INBOX")
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range box.Messages {
			if m.UID == uid {
				return m.Flags
			}
		}
		return nil
	}
	if seen(flags()) {
		t.Fatal("appended message unexpectedly seen")
	}
	if err = connector.SetSeen(context.Background(), ep, login, "INBOX", snapshot.UIDValidity, uid, true); err != nil {
		t.Fatal("confirmed seen write failed", err)
	}
	if !seen(flags()) {
		t.Fatal("seen write was not applied")
	}
	if err = connector.SetSeen(context.Background(), ep, login, "INBOX", snapshot.UIDValidity+1, uid, false); !errors.Is(err, mailnet.ErrFlagWrite) {
		t.Fatal("stale UIDVALIDITY accepted", err)
	}
	if !seen(flags()) {
		t.Fatal("stale write changed the provider")
	}
	if err = connector.SetSeen(context.Background(), ep, login, "INBOX", snapshot.UIDValidity, uid+500, true); !errors.Is(err, mailnet.ErrFlagWrite) {
		t.Fatal("unknown UID accepted", err)
	}
	if err = connector.SetSeen(context.Background(), ep, login, "Missing mailbox", snapshot.UIDValidity, uid, true); err == nil {
		t.Fatal("unknown mailbox accepted")
	}
	if err = connector.SetSeen(context.Background(), ep, login, "INBOX", snapshot.UIDValidity, uid, false); err != nil {
		t.Fatal("revert failed", err)
	}
	if seen(flags()) {
		t.Fatal("revert did not clear the flag")
	}
}

func seen(flags []string) bool {
	for _, flag := range flags {
		if flag == `\Seen` {
			return true
		}
	}
	return false
}
