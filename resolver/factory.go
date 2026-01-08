package resolver

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/ncruces/go-dns"
)

func FromURL(u string, caPool *x509.CertPool) (*net.Resolver, error) {
begin:
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	host := parsed.Hostname()
	port := parsed.Port()
	switch scheme := strings.ToLower(parsed.Scheme); scheme {
	case "":
		switch {
		case strings.HasPrefix(u, "//"):
			u = "dns:" + u
		default:
			u = "dns://" + u
		}
		goto begin
	case "udp", "dns":
		if port == "" {
			port = "53"
		}
		return NewPlainResolver(net.JoinHostPort(host, port)), nil
	case "tcp":
		if port == "" {
			port = "53"
		}
		return NewTCPResolver(net.JoinHostPort(host, port)), nil
	case "http", "https", "doh":
		if port == "" {
			if scheme == "http" {
				port = "80"
			} else {
				port = "443"
			}
		}
		if scheme == "doh" {
			parsed.Scheme = "https"
			u = parsed.String()
		}
		return dns.NewDoHResolver(u, dns.DoHAddresses(net.JoinHostPort(host, port)))
	case "tls", "dot":
		if port == "" {
			port = "853"
		}
		hp := net.JoinHostPort(host, port)
		return dns.NewDoTResolver(hp,
			dns.DoTAddresses(hp),
			dns.DoTConfig(&tls.Config{
				RootCAs: caPool,
			}),
		)
	default:
		return nil, errors.New("not implemented")
	}
}
