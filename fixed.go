package main

import (
	"context"
	"net"
)

type FixedDialer struct {
	fixedAddress string
	next ContextDialer
}

func NewFixedDialer(address string, next ContextDialer) *FixedDialer {
	return &FixedDialer{
		fixedAddress: address,
		next: next,
	}
}

func (d *FixedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	return d.next.DialContext(ctx, network, net.JoinHostPort(d.fixedAddress, port))
}

func (d *FixedDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}
