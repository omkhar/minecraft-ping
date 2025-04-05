package main

import (
	"github.com/iverly/go-mcping/mcping"
	"fmt"
	"time"
	"flag"
	"os"
)

func main() {
	serverPtr := flag.String("server","mc.hypixel.net","Minecraft server to ping")
	portPtr := flag.Int("port",25565,"Minecraft port")
	timePtr := flag.Duration("timeout",(5*time.Second),"Connection timeout")
	flag.Parse()
	pinger := mcping.NewPinger()
	response, err := pinger.PingWithTimeout(*serverPtr, uint16(*portPtr), (*timePtr) * time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("Ping time is %d\n",response.Latency)
}
