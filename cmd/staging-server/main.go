package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omkhar/minecraft-ping/v2/internal/stagingserver"
)

func bindFlags(fs *flag.FlagSet, cfg *stagingserver.Config) {
	fs.StringVar(&cfg.ListenIPv4, "listen4", "127.0.0.1:25565", "IPv4 listen address, or empty to disable")
	fs.StringVar(&cfg.ListenIPv6, "listen6", "[::1]:25566", "IPv6 listen address, or empty to disable")
	fs.StringVar(&cfg.BedrockListenIPv4, "bedrock-listen4", "", "Bedrock IPv4 listen address, or empty to disable")
	fs.StringVar(&cfg.BedrockListenIPv6, "bedrock-listen6", "", "Bedrock IPv6 listen address, or empty to disable")
	fs.StringVar(&cfg.StatusJSON, "status-json", stagingserver.DefaultStatusJSON(), "Status response JSON")
	fs.StringVar(&cfg.BedrockStatus, "bedrock-status", "", "Bedrock status response (defaults to a generated MCPE response when Bedrock listeners are enabled)")
	fs.DurationVar(&cfg.ConnectionDeadline, "deadline", 10*time.Second, "Per-connection read/write deadline")
}

func main() {
	cfg := stagingserver.Config{}
	bindFlags(flag.CommandLine, &cfg)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := stagingserver.Serve(ctx, cfg); err != nil && err != context.Canceled {
		stop()
		log.Print(err)
		os.Exit(1)
	}
	stop()
}
