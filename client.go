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

type pingRequest struct {
	target       endpoint
	explicitPort bool
	timeout      time.Duration
	options      pingOptions
}

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

func ping(target endpoint, timeout time.Duration, options pingOptions) (int, error) {
	if options.edition == editionBedrock {
		return pingBedrock(target, timeout, options)
	}
	return newPingClient().ping(target, timeout, options)
}

func defaultDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func newPingRequest(target targetSpec, timeout time.Duration, options pingOptions) (pingRequest, error) {
	ed := options.edition
	if ed != editionBedrock {
		ed = editionJava
	}
	normalized := newEndpoint(target.Host, target.defaultPort(options.addressFamily, ed))
	if target.PortExplicit {
		normalized = newEndpoint(target.Host, target.Port)
	}
	if err := normalized.validate(); err != nil {
		return pingRequest{}, err
	}
	if err := options.addressFamily.validate(); err != nil {
		return pingRequest{}, err
	}
	if timeout <= 0 {
		return pingRequest{}, fmt.Errorf("invalid timeout: %s. timeout must be greater than 0", timeout)
	}
	if timeout > maxAllowedTimeout {
		return pingRequest{}, fmt.Errorf("invalid timeout: %s. timeout must be less than or equal to %s", timeout, maxAllowedTimeout)
	}

	return pingRequest{
		target:       normalized,
		explicitPort: target.PortExplicit,
		timeout:      timeout,
		options:      options,
	}, nil
}

func (c pingClient) ping(target endpoint, timeout time.Duration, options pingOptions) (int, error) {
	c = c.withDefaults()

	request, err := newPingRequest(newTargetSpec(target.Host, target.Port, target.Port != defaultMinecraftPort), timeout, options)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), request.timeout)
	defer cancel()

	route, err := c.resolveJavaRouteContext(ctx, newTargetSpec(request.target.Host, request.target.Port, request.explicitPort))
	if err != nil {
		return 0, fmt.Errorf("failed to ping server %s: %w", request.target, err)
	}

	candidates, err := c.resolveDialCandidates(ctx, route.Dial, request.options)
	if err != nil {
		return 0, fmt.Errorf("failed to ping server %s: %w", request.target, err)
	}

	sample, err := c.pingJavaPreparedContext(ctx, route, candidates, request.timeout)
	if err != nil {
		if route.Dial != request.target {
			return 0, fmt.Errorf("failed to ping server %s (resolved to %s): %w", request.target, route.Dial, err)
		}
		return 0, fmt.Errorf("failed to ping server %s: %w", request.target, err)
	}

	return durationToLatencyMs(sample.latency), nil
}

func (c pingClient) resolveEndpoint(target endpoint, timeout time.Duration) endpointRoute {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	route, err := c.withDefaults().resolveJavaRouteContext(ctx, newTargetSpec(target.Host, target.Port, target.Port != defaultMinecraftPort))
	if err != nil {
		return endpointRoute{
			Dial:      target,
			Handshake: target,
		}
	}

	return route
}

func (c pingClient) pingEndpoint(route endpointRoute, timeout time.Duration, allowPrivate ...bool) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	options := pingOptions{}
	if len(allowPrivate) > 0 && allowPrivate[0] {
		options.allowPrivateAddresses = true
	}

	candidates, err := c.withDefaults().resolveDialCandidates(ctx, route.Dial, options)
	if err != nil {
		return 0, err
	}

	sample, err := c.withDefaults().pingJavaPreparedContext(ctx, route, candidates, timeout)
	if err != nil {
		return 0, err
	}
	return durationToLatencyMs(sample.latency), nil
}

func (c pingClient) dialMinecraftTCP(target endpoint, timeout time.Duration, allowPrivate bool) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return c.withDefaults().dialMinecraftTCPContext(ctx, target, pingOptions{
		allowPrivateAddresses: allowPrivate,
	})
}

func (c pingClient) dialMinecraftTCPContext(ctx context.Context, target endpoint, options pingOptions) (net.Conn, error) {
	candidates, err := c.resolveDialCandidates(ctx, target, options)
	if err != nil {
		return nil, err
	}

	return c.dialCandidates(ctx, candidates)
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
