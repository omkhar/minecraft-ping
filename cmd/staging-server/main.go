package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omkhar/minecraft-ping/internal/stagingserver"
)

func main() {
	cfg := stagingserver.Config{}

	flag.StringVar(&cfg.ListenIPv4, "listen4", "127.0.0.1:25565", "IPv4 listen address, or empty to disable")
	flag.StringVar(&cfg.ListenIPv6, "listen6", "[::1]:25566", "IPv6 listen address, or empty to disable")
	flag.StringVar(&cfg.StatusJSON, "status-json", stagingserver.DefaultStatusJSON(), "Status response JSON")
	flag.DurationVar(&cfg.ConnectionDeadline, "deadline", 10*time.Second, "Per-connection read/write deadline")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := stagingserver.Serve(ctx, cfg); err != nil && err != context.Canceled {
		stop()
		log.Print(err)
		os.Exit(1)
	}
	stop()
}
