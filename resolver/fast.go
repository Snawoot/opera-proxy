package resolver

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/hashicorp/go-multierror"
)

type LookupNetIPer interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type FastResolver struct {
	upstreams []LookupNetIPer
}

func FastFromURLs(urls ...string) (*FastResolver, error) {
	resolvers := make([]LookupNetIPer, 0, len(urls))
	for i, u := range urls {
		res, err := FromURL(u)
		if err != nil {
			return nil, fmt.Errorf("unable to construct resolver #%d (%q): %w", i, u, err)
		}
		resolvers = append(resolvers, res)
	}
	return NewFastResolver(resolvers...), nil
}

func NewFastResolver(resolvers ...LookupNetIPer) *FastResolver {
	return &FastResolver{
		upstreams: resolvers,
	}
}

func (r FastResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	masterNotInterested := make(chan struct{})
	defer close(masterNotInterested)
	errors := make(chan error)
	success := make(chan []netip.Addr)
	for _, res := range r.upstreams {
		go func(res LookupNetIPer) {
			addrs, err := res.LookupNetIP(ctx, network, host)
			if err == nil {
				select {
				case success <- addrs:
				case <-masterNotInterested:
				}
			} else {
				select {
				case errors <-err:
				case <-masterNotInterested:
				}
			}
		}(res)
	}

	var resErr error
	for _ = range r.upstreams {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case resAddrs := <-success:
			return resAddrs, nil
		case err := <-errors:
			resErr = multierror.Append(resErr, err)
		}
	}
	return nil, resErr
}
