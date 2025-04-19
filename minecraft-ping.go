package main

import (
    "flag"
    "fmt"
    "log"
    "time"

	"github.com/iverly/go-mcping/mcping"
)

func main() {
    serverPtr := flag.String("server", "mc.hypixel.net", "Minecraft server to ping (e.g., mc.example.com)")
    portPtr := flag.Int("port", 25565, "Minecraft server port (default: 25565)")
    timePtr := flag.Duration("timeout", 5*time.Second, "Connection timeout (e.g., 5s, 1m)")

	flag.Parse()

	if *portPtr < 1 || *portPtr > 65535 {
        log.Fatalf("Invalid port: %d. Port must be between 1 and 65535.", *portPtr)
    }

	pinger := mcping.NewPinger()
	response, err := pinger.PingWithTimeout(*serverPtr, uint16(*portPtr), *timePtr)
	
	if err != nil {
		log.Fatalf("Failed to ping server %s:%d - %v", *serverPtr, *portPtr, err)
	}

	fmt.Printf("Ping time is %d\n",response.Latency)
}
