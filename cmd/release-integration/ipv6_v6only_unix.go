//go:build !windows

package main

import "syscall"

func setIPv6Only(fd uintptr) error {
	// #nosec G115 -- raw socket file descriptors fit in int on supported unix platforms.
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
}
