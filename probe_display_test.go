package main

import (
	"context"
	"net/netip"
	"testing"
)

func TestPrepareJavaProbeMultipleCandidatesDefersDisplayAddressUntilSuccess(t *testing.T) {
	t.Parallel()

	prepared, err := prepareJavaProbe(
		context.Background(),
		pingClient{
			resolver: &stubResolver{
				ipAddrs: []netip.Addr{
					mustParseAddr(t, "2606:4700:4700::1111"),
					mustParseAddr(t, "8.8.8.8"),
				},
			},
		},
		targetSpec{Host: "example.com"},
		pingOptions{},
	)
	if err != nil {
		t.Fatalf("prepareJavaProbe() error = %v", err)
	}

	if got := prepared.banner(false); got != "PING example.com port 25565 [java]:" {
		t.Fatalf("banner(false) = %q", got)
	}
	if got := prepared.summaryLabel(true); got != "example.com" {
		t.Fatalf("summaryLabel(true) before success = %q", got)
	}

	prepared.observeSample(probeSample{remote: mustAddrPort(t, "8.8.8.8:25565")})
	if got := prepared.summaryLabel(true); got != "8.8.8.8" {
		t.Fatalf("summaryLabel(true) after success = %q", got)
	}
}

func TestPrepareBedrockProbeMultipleCandidatesDefersDisplayAddressUntilSuccess(t *testing.T) {
	t.Parallel()

	prepared, err := prepareBedrockProbe(
		context.Background(),
		pingClient{
			resolver: stubBedrockResolver{
				addrs: []netip.Addr{
					mustParseAddr(t, "2606:4700:4700::1111"),
					mustParseAddr(t, "8.8.8.8"),
				},
			},
		},
		targetSpec{Host: "example.com"},
		pingOptions{},
	)
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}

	if got := prepared.banner(false); got != "PING example.com port 19132 [bedrock]:" {
		t.Fatalf("banner(false) = %q", got)
	}
	if got := prepared.summaryLabel(true); got != "example.com" {
		t.Fatalf("summaryLabel(true) before success = %q", got)
	}

	prepared.observeSample(probeSample{remote: mustAddrPort(t, "8.8.8.8:19132")})
	if got := prepared.summaryLabel(true); got != "8.8.8.8" {
		t.Fatalf("summaryLabel(true) after success = %q", got)
	}
}
