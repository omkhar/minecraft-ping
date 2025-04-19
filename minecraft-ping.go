package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/iverly/go-mcping/mcping"
)

func pingServer(server string, port int, timeout time.Duration) (int, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port: %d. port must be between 1 and 65535", port)
	}

	pinger := mcping.NewPinger()
	response, err := pinger.PingWithTimeout(server, uint16(port), timeout)

	if err != nil {
		return 0, fmt.Errorf("failed to ping server %s:%d - %v", server, port, err)
	}

	return int(response.Latency), nil
}

func main() {
	serverPtr := flag.String("server", "mc.hypixel.net", "Minecraft server to ping (e.g., mc.example.com)")
	portPtr := flag.Int("port", 25565, "Minecraft server port (default: 25565)")
	timePtr := flag.Duration("timeout", 5*time.Second, "Connection timeout (e.g., 5s, 1m)")

	flag.Parse()

	latency, err := pingServer(*serverPtr, *portPtr, *timePtr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Ping time is %d\n", latency)
}
