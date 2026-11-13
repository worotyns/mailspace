// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"errors"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
	"time"
)

type SendResult string

const (
	Accepted SendResult = "accepted"
	Rejected SendResult = "rejected"
	Unknown  SendResult = "unknown"
)

type Sender interface {
	Send(context.Context, Endpoint, Login, string, []string, []byte) SendResult
	AppendSent(context.Context, Endpoint, Login, string, []byte) error
}

// Send never retries. Unknown includes a lost final DATA response: the remote
// server may already have accepted the message. Remote errors never escape.
func (c *Connector) Send(ctx context.Context, ep Endpoint, login Login, from string, to []string, raw []byte) SendResult {
	if len(raw) == 0 || len(raw) > MaxMIMEBytes || len(to) == 0 || len(to) > 100 {
		return Rejected
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, config, err := c.connection(ctx, ep)
	if err != nil {
		return Rejected
	}
	defer conn.Close()
	var client *smtp.Client
	if ep.TLS == "starttls" {
		client, err = smtp.NewClientStartTLS(conn, config)
	} else {
		client = smtp.NewClient(conn)
	}
	if err != nil {
		return Rejected
	}
	defer client.Close()
	client.CommandTimeout = 10 * time.Second
	client.SubmissionTimeout = 15 * time.Second
	if err = client.Auth(sasl.NewPlainClient("", login.Username, login.Password)); err != nil {
		return Rejected
	}
	if err = client.Mail(from, &smtp.MailOptions{Size: int64(len(raw))}); err != nil {
		return Rejected
	}
	for _, address := range to {
		if err = client.Rcpt(address, nil); err != nil {
			return Rejected
		}
	}
	data, err := client.Data()
	if err != nil {
		return Rejected
	}
	if _, err = data.Write(raw); err != nil {
		return Unknown
	}
	if err = data.Close(); err != nil {
		var rejected *smtp.SMTPError
		if errors.As(err, &rejected) && rejected.Code >= 400 && rejected.Code <= 599 {
			return Rejected
		}
		return Unknown
	}
	return Accepted
}
func (c *Connector) AppendSent(ctx context.Context, ep Endpoint, login Login, folder string, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	session, err := c.OpenRead(ctx, ep, login)
	if err != nil {
		return err
	}
	defer session.Close()
	reader := session.(*readSession)
	names, err := reader.Mailboxes()
	if err != nil {
		return err
	}
	found := false
	for _, name := range names {
		if name == folder {
			found = true
			break
		}
	}
	if !found {
		return CheckError{"IMAP", "Sent folder"}
	}
	cmd := reader.client.Append(folder, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}, Time: time.Now()})
	if _, err = cmd.Write(raw); err != nil {
		return CheckError{"IMAP", "Sent copy"}
	}
	if err = cmd.Close(); err != nil {
		return CheckError{"IMAP", "Sent copy"}
	}
	if _, err = cmd.Wait(); err != nil {
		return CheckError{"IMAP", "Sent copy"}
	}
	return nil
}
