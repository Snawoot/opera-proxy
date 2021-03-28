package main

import (
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/miekg/dns"
	"time"
)

type Resolver struct {
	upstream upstream.Upstream
}

const DOT = 0x2e

func NewResolver(address string, timeout time.Duration) (*Resolver, error) {
	opts := upstream.Options{Timeout: timeout}
	u, err := upstream.AddressToUpstream(address, opts)
	if err != nil {
		return nil, err
	}
	return &Resolver{upstream: u}, nil
}

func (r *Resolver) ResolveA(domain string) []string {
	res := make([]string, 0)
	if len(domain) == 0 {
		return res
	}
	if domain[len(domain)-1] != DOT {
		domain = domain + "."
	}
	req := dns.Msg{}
	req.Id = dns.Id()
	req.RecursionDesired = true
	req.Question = []dns.Question{
		{Name: domain, Qtype: dns.TypeA, Qclass: dns.ClassINET},
	}
	reply, err := r.upstream.Exchange(&req)
	if err != nil {
		return res
	}
	for _, rr := range reply.Answer {
		if a, ok := rr.(*dns.A); ok {
			res = append(res, a.A.String())
		}
	}
	return res
}

func (r *Resolver) ResolveAAAA(domain string) []string {
	res := make([]string, 0)
	if len(domain) == 0 {
		return res
	}
	if domain[len(domain)-1] != DOT {
		domain = domain + "."
	}
	req := dns.Msg{}
	req.Id = dns.Id()
	req.RecursionDesired = true
	req.Question = []dns.Question{
		{Name: domain, Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
	}
	reply, err := r.upstream.Exchange(&req)
	if err != nil {
		return res
	}
	for _, rr := range reply.Answer {
		if a, ok := rr.(*dns.AAAA); ok {
			res = append(res, a.AAAA.String())
		}
	}
	return res
}

func (r *Resolver) Resolve(domain string) []string {
	res := r.ResolveA(domain)
	if len(res) == 0 {
		res = r.ResolveAAAA(domain)
	}
	return res
}
