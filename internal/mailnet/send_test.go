// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestSMTPAcceptedRejectedAndLostDATAResponse(t *testing.T) {
	for _, scenario := range []struct {
		mode   string
		result string
		want   SendResult
	}{{"tls", "accepted", Accepted}, {"starttls", "accepted", Accepted}, {"tls", "rejected", Rejected}, {"tls", "lost-response", Unknown}, {"tls", "timeout", Unknown}, {"tls", "rcpt-rejected", Rejected}} {
		t.Run(scenario.mode+"/"+scenario.result, func(t *testing.T) {
			tlsConfig, roots := testTLS(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ep := endpointOf(t, listener, scenario.mode)
			if scenario.mode == "tls" {
				listener = tls.NewListener(listener, tlsConfig)
			}
			captured := make(chan []byte, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					captured <- nil
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(conn)
				encrypted := scenario.mode == "tls"
				var raw []byte
				defer func() { captured <- raw }()
				io.WriteString(conn, "220 synthetic.test ESMTP\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO "):
						if encrypted {
							io.WriteString(conn, "250-synthetic.test\r\n250 AUTH PLAIN\r\n")
						} else {
							io.WriteString(conn, "250-synthetic.test\r\n250 STARTTLS\r\n")
						}
					case strings.HasPrefix(line, "STARTTLS"):
						io.WriteString(conn, "220 Upgrade\r\n")
						secure := tls.Server(conn, tlsConfig)
						if secure.Handshake() != nil {
							return
						}
						conn = secure
						reader = bufio.NewReader(conn)
						encrypted = true
					case strings.HasPrefix(line, "AUTH PLAIN"):
						if !encrypted {
							return
						}
						io.WriteString(conn, "235 Authenticated\r\n")
					case strings.HasPrefix(line, "MAIL FROM:"):
						io.WriteString(conn, "250 Sender OK\r\n")
					case strings.HasPrefix(line, "RCPT TO:"):
						if scenario.result == "rcpt-rejected" {
							io.WriteString(conn, "550 Synthetic rejection\r\n")
						} else {
							io.WriteString(conn, "250 Recipient OK\r\n")
						}
					case strings.HasPrefix(line, "DATA"):
						io.WriteString(conn, "354 Send data\r\n")
						raw, err = textproto.NewReader(reader).ReadDotBytes()
						if err != nil {
							return
						}
						switch scenario.result {
						case "lost-response":
							return
						case "timeout":
							io.Copy(io.Discard, conn)
							return
						case "rejected":
							io.WriteString(conn, "550 Data rejected\r\n")
						default:
							io.WriteString(conn, "250 Queued\r\n")
						}
					default:
						return
					}
				}
			}()
			policy, err := NewPolicy("", ep.Address())
			if err != nil {
				t.Fatal(err)
			}
			connector := Connector{Policy: policy, Roots: roots}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result := connector.Send(ctx, ep, Login{Username: "synthetic", Password: "synthetic-only"}, "sender@example.test", []string{"recipient@example.test"}, []byte("From: sender@example.test\r\nTo: recipient@example.test\r\nSubject: Synthetic\r\n\r\nNo real delivery.\r\n"))
			if result != scenario.want {
				t.Fatal("wrong delivery certainty", result, scenario.want)
			}
			raw := <-captured
			if scenario.result == "rcpt-rejected" && len(raw) > 0 {
				t.Fatal("DATA sent after rejected recipient")
			}
			if scenario.result != "rcpt-rejected" && len(raw) == 0 {
				t.Fatal("fixture did not receive DATA")
			}
		})
	}
}
