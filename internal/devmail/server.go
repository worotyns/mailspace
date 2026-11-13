// SPDX-License-Identifier: AGPL-3.0-only
// Package devmail is an isolated, loopback-only development fixture, not a mail server product.
package devmail

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
	"io"
	"log"
	appmail "mailspace.local/mailspace/internal/mail"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	StateDir string
	IMAPAddr string
	SMTPAddr string
	Username string
}
type Server struct {
	IMAPAddr     string
	SMTPAddr     string
	CAFile       string
	PasswordFile string
	Username     string
	imap         *imapserver.Server
	smtp         *smtp.Server
}

func (s *Server) Close() error { _ = s.imap.Close(); return s.smtp.Close() }
func loopbackAddress(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func writeFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
func credentials(dir string) (string, string, *tls.Config, error) {
	passwordFile := filepath.Join(dir, "password")
	password, err := os.ReadFile(passwordFile)
	if errors.Is(err, os.ErrNotExist) {
		password = []byte(rand.Text())
		if err = writeFile(passwordFile, password, 0600); err != nil {
			return "", "", nil, err
		}
	} else if err != nil {
		return "", "", nil, err
	}
	info, err := os.Stat(passwordFile)
	if err != nil || info.Mode().Perm()&0077 != 0 || len(password) < 16 || len(password) > 128 {
		return "", "", nil, errors.New("invalid development password file")
	}
	certFile, keyFile := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "tls.key")
	if _, err = os.Stat(certFile); errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", "", nil, err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return "", "", nil, err
		}
		template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Mailspace local development fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
		certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			return "", "", nil, err
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", "", nil, err
		}
		if err = writeFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
			return "", "", nil, err
		}
		if err = writeFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0644); err != nil {
			return "", "", nil, err
		}
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", "", nil, err
	}
	return string(password), certFile, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}
func nextValidity(dir string) (uint32, error) {
	path := filepath.Join(dir, "uidvalidity")
	previous := uint64(0)
	if value, err := os.ReadFile(path); err == nil {
		previous, err = strconv.ParseUint(strings.TrimSpace(string(value)), 10, 32)
		if err != nil {
			return 0, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	next := uint64(time.Now().Unix())
	if previous >= next {
		next = previous + 1
	}
	if next > 1<<32-1 {
		return 0, errors.New("development UIDVALIDITY counter exhausted")
	}
	if err := os.WriteFile(path, []byte(strconv.FormatUint(next, 10)), 0600); err != nil {
		return 0, err
	}
	return uint32(next), nil
}

type memorySession struct {
	imapserver.Session
	validity uint32
}

func (s *memorySession) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	value, err := s.Session.Select(name, options)
	if err == nil {
		value.UIDValidity = s.validity
	}
	return value, err
}
func (s *memorySession) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	value, err := s.Session.Status(name, options)
	if err == nil {
		value.UIDValidity = s.validity
	}
	return value, err
}
func (s *memorySession) Append(name string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	value, err := s.Session.Append(name, r, options)
	if err == nil {
		value.UIDValidity = s.validity
	}
	return value, err
}
func Start(options Options) (*Server, error) {
	if !loopbackAddress(options.IMAPAddr) || !loopbackAddress(options.SMTPAddr) {
		return nil, errors.New("development mail fixture binds only to numeric loopback addresses")
	}
	address, err := mail.ParseAddress(options.Username)
	if err != nil || address.Address != options.Username || !strings.HasSuffix(strings.ToLower(options.Username), ".test") {
		return nil, errors.New("development user must use a synthetic .test address")
	}
	if err = os.MkdirAll(options.StateDir, 0700); err != nil {
		return nil, err
	}
	password, caFile, tlsConfig, err := credentials(options.StateDir)
	if err != nil {
		return nil, err
	}
	validity, err := nextValidity(options.StateDir)
	if err != nil {
		return nil, err
	}
	memory := imapmemserver.New()
	user := imapmemserver.NewUser(options.Username, password)
	for _, folder := range []string{"INBOX", "Sent", "Archive"} {
		if err = user.Create(folder, &imap.CreateOptions{}); err != nil {
			return nil, err
		}
	}
	for _, conversation := range appmail.Demo() {
		if conversation.Root().Encrypted {
			continue
		}
		folder := "INBOX"
		if conversation.Folder == "sent" {
			folder = "Sent"
		} else if conversation.Folder == "archive" {
			folder = "Archive"
		}
		for i, message := range conversation.Messages {
			flags := []imap.Flag{imap.FlagSeen}
			if conversation.Unread {
				flags = nil
			}
			raw, err := seedMIME(conversation, message, i)
			if err != nil {
				return nil, err
			}
			if _, err = user.Append(folder, bytes.NewReader(raw), &imap.AppendOptions{Flags: flags, Time: seedDate(message)}); err != nil {
				return nil, err
			}
		}
	}
	memory.AddUser(user)
	imapServer := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &memorySession{Session: memory.NewSession(), validity: validity}, nil, nil
	}, Logger: log.New(io.Discard, "", 0)})
	smtpServer := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &sinkSession{username: options.Username, password: password}, nil
	}))
	smtpServer.Domain = "mailspace.test"
	smtpServer.MaxMessageBytes = 8 << 20
	smtpServer.ErrorLog = log.New(io.Discard, "", 0)
	imapListener, err := tls.Listen("tcp", options.IMAPAddr, tlsConfig)
	if err != nil {
		return nil, err
	}
	smtpListener, err := tls.Listen("tcp", options.SMTPAddr, tlsConfig)
	if err != nil {
		imapListener.Close()
		return nil, err
	}
	server := &Server{IMAPAddr: imapListener.Addr().String(), SMTPAddr: smtpListener.Addr().String(), CAFile: caFile, PasswordFile: filepath.Join(options.StateDir, "password"), Username: options.Username, imap: imapServer, smtp: smtpServer}
	go imapServer.Serve(imapListener)
	go smtpServer.Serve(smtpListener)
	return server, nil
}

type sinkSession struct {
	username string
	password string
}

func (*sinkSession) Reset()        {}
func (*sinkSession) Logout() error { return nil }
func (*sinkSession) Mail(from string, _ *smtp.MailOptions) error {
	if !strings.HasSuffix(strings.ToLower(from), ".test") {
		return errors.New("development fixture accepts only .test envelopes")
	}
	return nil
}
func (*sinkSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	if !strings.HasSuffix(strings.ToLower(to), ".test") {
		return errors.New("development fixture accepts only .test envelopes")
	}
	return nil
}
func (*sinkSession) Data(r io.Reader) error {
	n, err := io.Copy(io.Discard, io.LimitReader(r, 8<<20+1))
	if err != nil {
		return err
	}
	if n > 8<<20 {
		return errors.New("development message too large")
	}
	return nil
}
func (*sinkSession) AuthMechanisms() []string { return []string{"PLAIN"} }
func (s *sinkSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_ string, username, password string) error {
		if username != s.username || subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) != 1 {
			return smtp.ErrAuthFailed
		}
		return nil
	}), nil
}
func seedMIME(conversation appmail.Conversation, message appmail.Message, index int) ([]byte, error) {
	var result bytes.Buffer
	subject := conversation.Subject
	if index > 0 && !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	date := seedDate(message)
	fmt.Fprintf(&result, "From: %s\r\nTo: team@northstar.test\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@mailspace.test>\r\nMIME-Version: 1.0\r\n", (&mail.Address{Name: message.Name, Address: message.Sender}).String(), mime.QEncoding.Encode("utf-8", subject), date.Format(time.RFC1123Z), message.ID)
	if index > 0 {
		fmt.Fprintf(&result, "In-Reply-To: <%s@mailspace.test>\r\nReferences: <%s@mailspace.test>\r\n", conversation.Root().ID, conversation.Root().ID)
	}
	if len(message.Attachments) == 0 {
		result.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		writer := quotedprintable.NewWriter(&result)
		_, _ = writer.Write([]byte(message.Text))
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return result.Bytes(), nil
	}
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	if err := writer.SetBoundary("mailspace-demo-" + message.ID); err != nil {
		return nil, err
	}
	body, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return nil, err
	}
	quoted := quotedprintable.NewWriter(body)
	_, _ = quoted.Write([]byte(message.Text))
	if err = quoted.Close(); err != nil {
		return nil, err
	}
	for _, attachment := range message.Attachments {
		filename := strings.TrimSuffix(attachment.Name, filepath.Ext(attachment.Name)) + ".txt"
		header := textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": filename})}, "Content-Transfer-Encoding": {"base64"}}
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		content := "Synthetic attachment placeholder for " + attachment.Name + ". No external file was fetched.\n"
		_, _ = io.WriteString(part, base64.StdEncoding.EncodeToString([]byte(content)))
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	fmt.Fprintf(&result, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", writer.Boundary())
	result.Write(multipartBody.Bytes())
	return result.Bytes(), nil
}

func seedDate(message appmail.Message) time.Time {
	date := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if parsed, err := time.Parse("15:04", message.Time); err == nil {
		date = time.Date(2026, 11, 12, parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
	} else if message.Time == "Yesterday" {
		date = date.AddDate(0, 0, -1)
	} else if message.Time == "Tuesday" {
		date = date.AddDate(0, 0, -2)
	}
	return date
}
