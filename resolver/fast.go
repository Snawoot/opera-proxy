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

type lookupReply struct {
	addrs []netip.Addr
	err   error
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
	ctx, cl := context.WithCancel(ctx)
	drain := make(chan lookupReply, len(r.upstreams))
	for _, res := range r.upstreams {
		go func(res LookupNetIPer) {
			addrs, err := res.LookupNetIP(ctx, network, host)
			drain <- lookupReply{addrs, err}
		}(res)
	}

	i := 0
	var resAddrs []netip.Addr
	var resErr error
	for ; i < len(r.upstreams); i++ {
		pair := <-drain
		if pair.err != nil {
			resErr = multierror.Append(resErr, pair.err)
		} else {
			cl()
			resAddrs = pair.addrs
			resErr = nil
			break
		}
	}
	go func() {
		for i = i + 1; i < len(r.upstreams); i++ {
			<-drain
		}
	}()
	return resAddrs, resErr
}
