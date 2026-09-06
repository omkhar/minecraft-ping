package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const (
	maxAllowedTimeout        = 30 * time.Second
	defaultDialFallbackDelay = 250 * time.Millisecond
)

type dnsResolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type pingClient struct {
	resolver          dnsResolver
	dialContext       dialContextFunc
	tokenSource       func() (uint64, error)
	now               func() time.Time
	dialFallbackDelay time.Duration
}

func newPingClient() pingClient {
	return pingClient{
		resolver:          net.DefaultResolver,
		dialContext:       defaultDialContext,
		tokenSource:       generatePingToken,
		now:               time.Now,
		dialFallbackDelay: defaultDialFallbackDelay,
	}
}

func (c pingClient) withDefaults() pingClient {
	if c.resolver == nil {
		c.resolver = net.DefaultResolver
	}
	if c.dialContext == nil {
		c.dialContext = defaultDialContext
	}
	if c.tokenSource == nil {
		c.tokenSource = generatePingToken
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.dialFallbackDelay <= 0 {
		c.dialFallbackDelay = defaultDialFallbackDelay
	}
	return c
}

func defaultDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func (c pingClient) resolveDialCandidates(ctx context.Context, target endpoint, options pingOptions) ([]dialCandidate, error) {
	if _, ok := target.literalIP(); ok {
		return dialCandidateForLiteralIP(target, options)
	}

	port, err := target.uint16Port()
	if err != nil {
		return nil, err
	}

	addrs, err := c.resolver.LookupNetIP(ctx, options.addressFamily.resolverNetwork(), target.Host)
	if err != nil {
		return nil, err
	}

	return dialCandidatesForResolvedIPs(target.Host, port, addrs, options)
}

type dialAttemptResult struct {
	candidate dialCandidate
	conn      net.Conn
	err       error
}

func (c pingClient) dialCandidates(ctx context.Context, candidates []dialCandidate) (net.Conn, error) {
	if len(candidates) == 0 {
		return nil, errors.New("no dial candidates available")
	}
	if len(candidates) == 1 {
		candidate := candidates[0]
		return c.dialContext(ctx, candidate.Network(), candidate.String())
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan dialAttemptResult, len(candidates))
	for i, candidate := range candidates {
		delay := time.Duration(i) * c.dialFallbackDelay
		go c.dialCandidateAfterDelay(ctx, candidate, delay, results)
	}

	errs := make([]error, 0, len(candidates))
	for remaining := len(candidates); remaining > 0; remaining-- {
		result := <-results
		if result.err == nil {
			cancel()
			return result.conn, nil
		}

		errs = append(errs, fmt.Errorf("%s: %w", result.candidate, result.err))
	}

	if len(errs) == 0 {
		return nil, errors.New("failed to dial any resolved address")
	}

	return nil, errors.Join(errs...)
}

func (c pingClient) dialCandidateAfterDelay(ctx context.Context, candidate dialCandidate, delay time.Duration, results chan<- dialAttemptResult) {
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			results <- dialAttemptResult{candidate: candidate, err: ctx.Err()}
			return
		case <-timer.C:
		}
	}

	conn, err := c.dialContext(ctx, candidate.Network(), candidate.String())
	if err != nil {
		results <- dialAttemptResult{candidate: candidate, err: err}
		return
	}

	if ctx.Err() != nil {
		_ = conn.Close()
		results <- dialAttemptResult{candidate: candidate, err: ctx.Err()}
		return
	}

	results <- dialAttemptResult{candidate: candidate, conn: conn}
}
