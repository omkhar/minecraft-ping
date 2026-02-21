package main

import (
	"flag"
	"fmt"
	"log"
	"time"
)

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
