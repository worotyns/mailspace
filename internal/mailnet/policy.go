// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

var ErrEndpoint = errors.New("mail endpoint is not allowed by the administrator")

type Endpoint struct {
	Host string
	Port int
	TLS  string
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }
func NormalizeEndpoint(endpoint Endpoint) (Endpoint, error) {
	for _, c := range endpoint.Host {
		if c < 32 || c == 127 {
			return Endpoint{}, ErrEndpoint
		}
	}
	endpoint.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(endpoint.Host), "."))
	if endpoint.Port < 1 || endpoint.Port > 65535 || (endpoint.TLS != "tls" && endpoint.TLS != "starttls") {
		return Endpoint{}, ErrEndpoint
	}
	if addr, err := netip.ParseAddr(endpoint.Host); err == nil {
		if addr.Zone() != "" {
			return Endpoint{}, ErrEndpoint
		}
		endpoint.Host = addr.Unmap().String()
		return endpoint, nil
	}
	if endpoint.Host == "" || len(endpoint.Host) > 253 {
		return Endpoint{}, ErrEndpoint
	}
	for _, label := range strings.Split(endpoint.Host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return Endpoint{}, ErrEndpoint
		}
		for _, char := range label {
			if char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return Endpoint{}, ErrEndpoint
			}
		}
	}
	return endpoint, nil
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type tcpDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type Policy struct {
	dialer   tcpDialer
	public   map[string]bool
	private  map[string]bool
	resolver Resolver
}

func NewPolicy(publicEntries, privateEntries string) (*Policy, error) {
	p := &Policy{public: map[string]bool{}, private: map[string]bool{}, resolver: net.DefaultResolver, dialer: &net.Dialer{Timeout: 5 * time.Second}}
	for _, group := range []struct {
		value  string
		target map[string]bool
	}{{publicEntries, p.public}, {privateEntries, p.private}} {
		for _, entry := range strings.Split(group.value, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			host, port, err := net.SplitHostPort(entry)
			if err != nil {
				return nil, ErrEndpoint
			}
			number, err := strconv.Atoi(port)
			if err != nil {
				return nil, ErrEndpoint
			}
			ep, err := NormalizeEndpoint(Endpoint{Host: host, Port: number, TLS: "tls"})
			if err != nil {
				return nil, err
			}
			group.target[ep.Address()] = true
		}
	}
	return p, nil
}

var specialRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range specialRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// resolve validates every result. Dial uses these numeric addresses directly,
// so a second DNS lookup cannot rebind the host between checking and connecting.
func (p *Policy) resolve(ctx context.Context, ep Endpoint) ([]netip.Addr, error) {
	normalized, err := NormalizeEndpoint(ep)
	if err != nil {
		return nil, err
	}
	allowPrivate := p.private[normalized.Address()]
	if !allowPrivate && !p.public[normalized.Address()] {
		return nil, ErrEndpoint
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(normalized.Host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = p.resolver.LookupNetIP(ctx, "ip", normalized.Host)
		if err != nil {
			return nil, ErrEndpoint
		}
	}
	if len(ips) == 0 {
		return nil, ErrEndpoint
	}
	for _, ip := range ips {
		if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || (!allowPrivate && !publicIP(ip)) {
			return nil, ErrEndpoint
		}
	}
	return ips, nil
}
func (p *Policy) dial(ctx context.Context, ep Endpoint) (net.Conn, error) {
	ips, err := p.resolve(ctx, ep)
	if err != nil {
		return nil, err
	}
	dialer := p.dialer
	if dialer == nil {
		return nil, ErrEndpoint
	}
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(ep.Port)))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("mail endpoint connection failed")
}

func (p *Policy) HasEndpoints() bool { return len(p.public)+len(p.private) > 0 }
