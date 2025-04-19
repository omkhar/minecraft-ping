package main

import (
	"testing"
	"time"
)

func TestPingServer(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		port    int
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "Valid server and port",
			server:  "mc.hypixel.net",
			port:    25565,
			timeout: 5 * time.Second,
			wantErr: false,
		},
		{
			name:    "Invalid port - too low",
			server:  "mc.hypixel.net",
			port:    0,
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid port - too high",
			server:  "mc.hypixel.net",
			port:    65536,
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid server",
			server:  "nonexistent.server",
			port:    25565,
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid timeout",
			server:  "mc.hypixel.net",
			port:    25565,
			timeout: -1 * time.Second,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latency, err := pingServer(tt.server, tt.port, tt.timeout)

			if tt.wantErr {
				if err == nil {
					t.Errorf("pingServer() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("pingServer() error = %v, wantErr %v", err, tt.wantErr)
				}
				if latency <= 0 {
					t.Errorf("pingServer() got invalid latency: %d", latency)
				}
			}
		})
	}
}
