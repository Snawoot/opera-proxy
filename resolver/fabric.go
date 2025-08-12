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
	switch strings.ToLower(parsed.Scheme) {
	case "", "dns":
		host := parsed.Hostname()
		port := parsed.Port()
		if port == "" {
			port = "53"
		}
		return NewPlainResolver(net.JoinHostPort(host, port)), nil
	case "tcp":
		host := parsed.Hostname()
		port := parsed.Port()
		if port == "" {
			port = "53"
		}
		return NewTCPResolver(net.JoinHostPort(host, port)), nil
	case "http", "https":
		return dns.NewDoHResolver(u)
	case "tls":
		host := parsed.Hostname()
		port := parsed.Port()
		if port == "" {
			port = "853"
		}
		return dns.NewDoTResolver(net.JoinHostPort(host, port))
	default:
		return nil, errors.New("not implemented")
	}
}
