// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	server := httptest.NewTLSServer(nil)
	certificates := server.TLS.Certificates
	root := server.Certificate()
	server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(root)
	return &tls.Config{Certificates: certificates, MinVersion: tls.VersionTLS12}, pool
}
func endpointOf(t *testing.T, listener net.Listener, mode string) Endpoint {
	t.Helper()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	number, _ := strconv.Atoi(port)
	return Endpoint{Host: host, Port: number, TLS: mode}
}

type smtpSession struct{}

func (smtpSession) Reset()        {}
func (smtpSession) Logout() error { return nil }
func (smtpSession) Mail(string, *smtp.MailOptions) error {
	return errors.New("sending disabled in fixture")
}
func (smtpSession) Rcpt(string, *smtp.RcptOptions) error {
	return errors.New("sending disabled in fixture")
}
func (smtpSession) Data(io.Reader) error     { return errors.New("sending disabled in fixture") }
func (smtpSession) AuthMechanisms() []string { return []string{"PLAIN"} }
func (smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		if username != "fixture" || password != "synthetic mail password" {
			return errors.New("rejected " + password)
		}
		return nil
	}), nil
}
func TestVerifiedTLSAndAuthenticatedProbes(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			imapListener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			smtpListener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			imapEndpoint := endpointOf(t, imapListener, mode)
			smtpEndpoint := endpointOf(t, smtpListener, mode)
			memory := imapmemserver.New()
			memory.AddUser(imapmemserver.NewUser("fixture", "synthetic mail password"))
			imapServer := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
				return memory.NewSession(), nil, nil
			}, TLSConfig: tlsConfig, Logger: log.New(io.Discard, "", 0)})
			smtpServer := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) { return smtpSession{}, nil }))
			smtpServer.Domain = "fixture.test"
			smtpServer.TLSConfig = tlsConfig
			smtpServer.ErrorLog = log.New(io.Discard, "", 0)
			if mode == "tls" {
				imapListener = tls.NewListener(imapListener, tlsConfig)
				smtpListener = tls.NewListener(smtpListener, tlsConfig)
			}
			go imapServer.Serve(imapListener)
			go smtpServer.Serve(smtpListener)
			t.Cleanup(func() { imapServer.Close(); smtpServer.Close() })
			policy, err := NewPolicy("", imapEndpoint.Address()+","+smtpEndpoint.Address())
			if err != nil {
				t.Fatal(err)
			}
			connector := Connector{Policy: policy, Roots: roots, Timeout: 2 * time.Second}
			login := Login{Username: "fixture", Password: "synthetic mail password"}
			if err = connector.CheckIMAP(context.Background(), imapEndpoint, login); err != nil {
				t.Fatal(err)
			}
			if err = connector.CheckSMTP(context.Background(), smtpEndpoint, login); err != nil {
				t.Fatal(err)
			}
			wrong := Login{Username: "fixture", Password: "DO-NOT-LOG-THIS-PASSWORD"}
			if err = connector.CheckSMTP(context.Background(), smtpEndpoint, wrong); err == nil || strings.Contains(err.Error(), wrong.Password) {
				t.Fatal("authentication failure missing or leaks credentials", err)
			}
			connector.Roots = x509.NewCertPool()
			if err = connector.CheckIMAP(context.Background(), imapEndpoint, login); err == nil {
				t.Fatal("untrusted certificate accepted")
			}
		})
	}
}
func TestMissingSTARTTLSNeverSendsCredentials(t *testing.T) {
	for _, protocol := range []string{"imap", "smtp"} {
		t.Run(protocol, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			endpoint := endpointOf(t, listener, "starttls")
			captured := make(chan string, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					captured <- ""
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				if protocol == "imap" {
					io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1] fixture ready\r\n")
				} else {
					io.WriteString(conn, "220 fixture.test ESMTP\r\n")
				}
				reader := bufio.NewReader(conn)
				line, _ := reader.ReadString('\n')
				transcript := line
				if protocol == "smtp" {
					io.WriteString(conn, "250-fixture.test\r\n250 AUTH PLAIN\r\n")
				} else {
					fields := strings.Fields(line)
					if len(fields) > 0 {
						io.WriteString(conn, fields[0]+" NO STARTTLS unavailable\r\n")
					}
				}
				rest, _ := io.ReadAll(reader)
				captured <- transcript + string(rest)
			}()
			policy, _ := NewPolicy("", endpoint.Address())
			connector := Connector{Policy: policy, Timeout: 250 * time.Millisecond}
			login := Login{Username: "fixture", Password: "DO-NOT-SEND-BEFORE-TLS"}
			if protocol == "imap" {
				err = connector.CheckIMAP(context.Background(), endpoint, login)
			} else {
				err = connector.CheckSMTP(context.Background(), endpoint, login)
			}
			if err == nil {
				t.Fatal("STARTTLS downgrade accepted")
			}
			wire := <-captured
			if strings.Contains(wire, login.Password) || strings.Contains(strings.ToUpper(wire), "AUTH PLAIN ") {
				t.Fatal("credentials sent before TLS")
			}
		})
	}
}

func TestCancellationClosesUnresponsiveServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := endpointOf(t, listener, "starttls")
	accepted := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(accepted)
			close(closed)
			return
		}
		defer conn.Close()
		close(accepted)
		_, _ = io.Copy(io.Discard, conn)
		close(closed)
	}()
	policy, _ := NewPolicy("", endpoint.Address())
	connector := Connector{Policy: policy, Timeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- connector.CheckSMTP(ctx, endpoint, Login{Username: "fixture", Password: "not-transmitted"})
	}()
	<-accepted
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled check succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not interrupt protocol greeting")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled connection remained open")
	}
}
