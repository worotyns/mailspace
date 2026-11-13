// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
	"net"
	"time"
)

// Login values are deliberately excluded from JSON and generic formatting.
type Login struct {
	Username string `json:"-"`
	Password string `json:"-"`
}

func (Login) String() string   { return "<redacted mail credentials>" }
func (Login) GoString() string { return "<redacted mail credentials>" }

type CheckError struct {
	Protocol string
	Stage    string
}

func (e CheckError) Error() string { return e.Protocol + " connection check failed at " + e.Stage }

type Connector struct {
	Policy  *Policy
	Roots   *x509.CertPool
	Timeout time.Duration
}

func (c *Connector) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 10 * time.Second
}

type cancellableConn struct {
	net.Conn
	stop func() bool
}

func (c *cancellableConn) Close() error { c.stop(); return c.Conn.Close() }
func (c *Connector) connection(ctx context.Context, endpoint Endpoint) (net.Conn, *tls.Config, error) {
	ep, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return nil, nil, err
	}
	if c.Policy == nil {
		return nil, nil, ErrEndpoint
	}
	raw, err := c.Policy.dial(ctx, ep)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	conn := &cancellableConn{Conn: raw, stop: stop}
	config := &tls.Config{ServerName: ep.Host, RootCAs: c.Roots, MinVersion: tls.VersionTLS12}
	if ep.TLS == "tls" {
		encrypted := tls.Client(conn, config)
		if err = encrypted.HandshakeContext(ctx); err != nil {
			encrypted.Close()
			return nil, nil, errors.New("TLS verification failed")
		}
		return encrypted, config, nil
	}
	return conn, config, nil
}
func (c *Connector) CheckIMAP(ctx context.Context, endpoint Endpoint, login Login) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	conn, tlsConfig, err := c.connection(ctx, endpoint)
	if err != nil {
		return CheckError{Protocol: "IMAP", Stage: "endpoint/TLS"}
	}
	defer conn.Close()
	options := &imapclient.Options{TLSConfig: tlsConfig} // Never enable DebugWriter: it includes credentials.
	var client *imapclient.Client
	if endpoint.TLS == "starttls" {
		client, err = imapclient.NewStartTLS(conn, options)
	} else {
		client = imapclient.New(conn, options)
	}
	if err != nil {
		return CheckError{Protocol: "IMAP", Stage: "STARTTLS"}
	}
	defer client.Close()
	if client.Caps().Has(imap.AuthCap("PLAIN")) {
		err = client.Authenticate(sasl.NewPlainClient("", login.Username, login.Password))
	} else {
		err = client.Login(login.Username, login.Password).Wait()
	}
	if err != nil {
		return CheckError{Protocol: "IMAP", Stage: "authentication"}
	}
	// Closing, rather than waiting for a remote LOGOUT response, bounds the probe.
	return nil
}
func (c *Connector) CheckSMTP(ctx context.Context, endpoint Endpoint, login Login) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	conn, tlsConfig, err := c.connection(ctx, endpoint)
	if err != nil {
		return CheckError{Protocol: "SMTP", Stage: "endpoint/TLS"}
	}
	defer conn.Close()
	var client *smtp.Client
	if endpoint.TLS == "starttls" {
		client, err = smtp.NewClientStartTLS(conn, tlsConfig)
	} else {
		client = smtp.NewClient(conn)
	}
	if err != nil {
		return CheckError{Protocol: "SMTP", Stage: "STARTTLS"}
	}
	defer client.Close()
	client.CommandTimeout = c.timeout()
	client.SubmissionTimeout = c.timeout()
	if err = client.Auth(sasl.NewPlainClient("", login.Username, login.Password)); err != nil {
		return CheckError{Protocol: "SMTP", Stage: "authentication"}
	}
	return nil
}
