//go:build windows

package main

func setIPv6Only(uintptr) error {
	return nil
}
