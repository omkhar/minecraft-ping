package main

import (
	"context"
	"fmt"
)

func prepareProbe(ctx context.Context, cfg cliConfig) (preparedProbe, error) {
	switch cfg.Edition {
	case editionBedrock:
		return prepareBedrockProbe(ctx, newBedrockClient(), cfg.Target, cfg.Options)
	case editionJava:
		return prepareJavaProbe(ctx, newPingClient(), cfg.Target, cfg.Options)
	default:
		return nil, fmt.Errorf("unsupported edition %q", cfg.Edition)
	}
}
