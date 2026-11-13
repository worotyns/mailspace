// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

type fixedResolver []netip.Addr

func (f fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return f, nil
}
func TestEndpointPolicyAndRebinding(t *testing.T) {
	policy, err := NewPolicy("imap.example.test:993", "localhost:1993")
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "192.168.1.1", "198.18.0.1", "168.63.129.16", "::1", "fc00::1", "::ffff:127.0.0.1", "2001:db8::1"} {
		policy.resolver = fixedResolver{netip.MustParseAddr(ip)}
		if _, err = policy.resolve(context.Background(), Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}); err == nil {
			t.Fatalf("private/special IP allowed: %s", ip)
		}
	}
	policy.resolver = fixedResolver{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}
	if _, err = policy.resolve(context.Background(), Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}); err == nil {
		t.Fatal("mixed public/private DNS results accepted")
	}
	policy.resolver = fixedResolver{netip.MustParseAddr("8.8.8.8")}
	if _, err = policy.resolve(context.Background(), Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}); err != nil {
		t.Fatal("approved public endpoint blocked")
	}
	if _, err = policy.resolve(context.Background(), Endpoint{Host: "imap.example.test", Port: 80, TLS: "tls"}); err == nil {
		t.Fatal("unapproved port accepted")
	}
	policy.resolver = fixedResolver{netip.MustParseAddr("127.0.0.1")}
	if _, err = policy.resolve(context.Background(), Endpoint{Host: "localhost", Port: 1993, TLS: "tls"}); err != nil {
		t.Fatal("explicit admin LAN exception rejected")
	}
	empty, _ := NewPolicy("", "")
	if _, err = empty.resolve(context.Background(), Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}); err == nil {
		t.Fatal("default policy allows egress")
	}
}
func TestEndpointSyntax(t *testing.T) {
	for _, host := range []string{"https://mail.test/path", "mail.test@127.0.0.1", "mail.test\r\n", "mail.test/secret", "../mail.test", "bad_label.test"} {
		if _, err := NormalizeEndpoint(Endpoint{Host: host, Port: 993, TLS: "tls"}); err == nil {
			t.Fatalf("bad endpoint accepted %q", host)
		}
	}
	if _, err := NormalizeEndpoint(Endpoint{Host: "mail.test", Port: 143, TLS: "none"}); err == nil {
		t.Fatal("plaintext transport accepted")
	}
}

type countResolver struct{ calls int }

func (r *countResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	if r.calls > 1 {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

type recordingDialer struct{ address string }

func (d *recordingDialer) DialContext(_ context.Context, _ string, address string) (net.Conn, error) {
	d.address = address
	return nil, errors.New("fixture does not dial")
}
func TestDialPinsValidatedAddressWithoutResolvingTwice(t *testing.T) {
	policy, err := NewPolicy("imap.example.test:993", "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := &countResolver{}
	dialer := &recordingDialer{}
	policy.resolver = resolver
	policy.dialer = dialer
	_, _ = policy.dial(context.Background(), Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"})
	if resolver.calls != 1 || dialer.address != "8.8.8.8:993" {
		t.Fatal("validated DNS address not pinned", resolver.calls, dialer.address)
	}
	denied, _ := NewPolicy("", "")
	denied.resolver = resolver
	denied.dialer = dialer
	before := resolver.calls
	_, _ = denied.dial(context.Background(), Endpoint{Host: "unapproved.example.test", Port: 993, TLS: "tls"})
	if resolver.calls != before {
		t.Fatal("default-deny policy leaked DNS query")
	}
}
