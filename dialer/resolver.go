package dialer

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/hashicorp/go-multierror"
)

type Resolver struct {
	resolvers upstream.ParallelResolver
	timeout   time.Duration
}

func NewResolver(addresses []string, timeout time.Duration) (*Resolver, error) {
	resolvers := make([]upstream.Resolver, 0, len(addresses))
	opts := &upstream.Options{
		Timeout: timeout,
	}
	for _, addr := range addresses {
		u, err := upstream.AddressToUpstream(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("unable to construct upstream resolver from string %q: %w",
				addr, err)
		}
		resolvers = append(resolvers, &upstream.UpstreamResolver{Upstream: u})
	}
	return &Resolver{
		resolvers: resolvers,
		timeout:   timeout,
	}, nil
}

func (r *Resolver) LookupNetIP(ctx context.Context, network string, host string) (addrs []netip.Addr, err error) {
	return r.resolvers.LookupNetIP(ctx, network, host)
}

func (r *Resolver) Close() error {
	var res error
	for _, resolver := range r.resolvers {
		if closer, ok := resolver.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				res = multierror.Append(res, err)
			}
		}
	}
	return res
}

type LookupNetIPer interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ResolvingDialer struct {
	lookup LookupNetIPer
	next   ContextDialer
}

func NewResolvingDialer(lookup LookupNetIPer, next ContextDialer) *ResolvingDialer {
	return &ResolvingDialer{
		lookup: lookup,
		next:   next,
	}
}

func (d *ResolvingDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *ResolvingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("failed to extract host and port from %s: %w", address, err)
	}

	var resolveNetwork string
	switch network {
	case "udp4", "tcp4", "ip4":
		resolveNetwork = "ip4"
	case "udp6", "tcp6", "ip6":
		resolveNetwork = "ip6"
	case "udp", "tcp", "ip":
		resolveNetwork = "ip"
	default:
		return nil, fmt.Errorf("resolving dial %q: unsupported network %q", address, network)
	}
	resolved, err := d.lookup.LookupNetIP(ctx, resolveNetwork, host)
	if err != nil {
		return nil, fmt.Errorf("dial failed on address lookup: %w", err)
	}

	var conn net.Conn
	for _, ip := range resolved {
		conn, err = d.next.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("failed to dial %s: %w", address, err)
}
