package handler

import (
	"context"
	"log"
	"net"

	"github.com/Snawoot/opera-proxy/dialer"
	"github.com/things-go/go-socks5"
)

func NewSocksServer(dialer dialer.ContextDialer, logger *log.Logger) (*socks5.Server, error) {
	opts := []socks5.Option{
		socks5.WithLogger(socks5.NewLogger(logger)),
		socks5.WithRule(
			&socks5.PermitCommand{
				EnableConnect: true,
			},
		),
		socks5.WithDial(dialer.DialContext),
		socks5.WithResolver(DummySocksResolver{}),
	}
	return socks5.NewServer(opts...), nil
}

type DummySocksResolver struct{}

func (_ DummySocksResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, nil, nil
}
