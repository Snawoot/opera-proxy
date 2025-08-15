package resolver

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/ncruces/go-dns"
)

func FromURL(u string) (*net.Resolver, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	host := parsed.Hostname()
	port := parsed.Port()
	switch strings.ToLower(parsed.Scheme) {
	case "", "dns":
		if port == "" {
			port = "53"
		}
		return NewPlainResolver(net.JoinHostPort(host, port)), nil
	case "tcp":
		if port == "" {
			port = "53"
		}
		return NewTCPResolver(net.JoinHostPort(host, port)), nil
	case "http", "https":
		if port == "" {
			port = "443"
		}
		return dns.NewDoHResolver(u, dns.DoHAddresses(net.JoinHostPort(host, port)))
	case "tls":
		if port == "" {
			port = "853"
		}
		hp := net.JoinHostPort(host, port)
		return dns.NewDoTResolver(hp, dns.DoTAddresses(hp))
	default:
		return nil, errors.New("not implemented")
	}
}
