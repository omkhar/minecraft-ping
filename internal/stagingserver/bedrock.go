package stagingserver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	bedrockUnconnectedPingPacketID byte = 0x01
	bedrockUnconnectedPongPacketID byte = 0x1c
	bedrockServerGUID                   = uint64(1)
	bedrockProbeClientGUID              = uint64(1)
	maxBedrockStatusLength              = math.MaxUint16
)

var bedrockMagic = [16]byte{
	0x00, 0xff, 0xff, 0x00,
	0xfe, 0xfe, 0xfe, 0xfe,
	0xfd, 0xfd, 0xfd, 0xfd,
	0x12, 0x34, 0x56, 0x78,
}

func DefaultBedrockStatus(ipv4Port, ipv6Port int) string {
	return fmt.Sprintf("MCPE;minecraft-ping staging;924;1.26.3;0;10;1;staging;Creative;1;%d;%d;0;", ipv4Port, ipv6Port)
}

func validateBedrockStatus(status string) error {
	if status == "" {
		return errors.New("bedrock status must not be empty")
	}
	if !utf8.ValidString(status) {
		return errors.New("bedrock status must be valid UTF-8")
	}
	if len(status) > maxBedrockStatusLength {
		return fmt.Errorf("bedrock status exceeds maximum size: %d", len(status))
	}
	if !strings.HasPrefix(status, "MCPE;") {
		return errors.New("bedrock status must start with MCPE;")
	}
	return nil
}

func configuredPort(address string, fallback int) int {
	if address == "" {
		return fallback
	}

	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fallback
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > math.MaxUint16 {
		return fallback
	}
	return port
}

func defaultBedrockStatusForListeners(listeners []net.PacketConn) string {
	ipv4Port := 19132
	ipv6Port := 19133

	for _, listener := range listeners {
		addr, ok := listener.LocalAddr().(*net.UDPAddr)
		if !ok {
			continue
		}
		if addr.IP.To4() != nil {
			ipv4Port = addr.Port
			continue
		}
		if addr.IP.To16() != nil {
			ipv6Port = addr.Port
		}
	}

	return DefaultBedrockStatus(ipv4Port, ipv6Port)
}

func serveBedrockListener(ctx context.Context, conn net.PacketConn, status string, errCh chan<- error) {
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var buf [2048]byte
	for {
		n, addr, err := conn.ReadFrom(buf[:])
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case errCh <- err:
			default:
			}
			return
		}

		response, err := buildBedrockStatusResponse(buf[:n], status)
		if err != nil {
			continue
		}

		if _, err := conn.WriteTo(response, addr); err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case errCh <- err:
			default:
			}
			return
		}
	}
}

func buildBedrockStatusResponse(request []byte, status string) ([]byte, error) {
	pingTime, err := parseBedrockUnconnectedPing(request)
	if err != nil {
		return nil, err
	}
	return encodeBedrockUnconnectedPong(pingTime, status)
}

func parseBedrockUnconnectedPing(payload []byte) (uint64, error) {
	if len(payload) != 33 {
		return 0, fmt.Errorf("unexpected bedrock ping length: %d", len(payload))
	}
	if payload[0] != bedrockUnconnectedPingPacketID {
		return 0, fmt.Errorf("unexpected bedrock ping packet id: %d", payload[0])
	}
	if !slices.Equal(payload[9:25], bedrockMagic[:]) {
		return 0, errors.New("bedrock ping magic mismatch")
	}
	return binary.BigEndian.Uint64(payload[1:9]), nil
}

func encodeBedrockUnconnectedPong(pingTime uint64, status string) ([]byte, error) {
	if err := validateBedrockStatus(status); err != nil {
		return nil, err
	}

	payload := make([]byte, 0, 35+len(status))
	payload = append(payload, bedrockUnconnectedPongPacketID)

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], pingTime)
	payload = append(payload, scratch[:]...)

	binary.BigEndian.PutUint64(scratch[:], bedrockServerGUID)
	payload = append(payload, scratch[:]...)
	payload = append(payload, bedrockMagic[:]...)

	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(status))) // #nosec G115 -- bounded by validateBedrockStatus.
	payload = append(payload, length[:]...)
	payload = append(payload, status...)
	return payload, nil
}

func ProbeBedrock(network, host string, port int, timeout time.Duration) error {
	if port < 0 || port > math.MaxUint16 {
		return fmt.Errorf("invalid port %d", port)
	}

	address := net.JoinHostPort(host, fmt.Sprint(port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial(network, address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	pingTime := uint64(time.Now().UnixMilli())
	request := make([]byte, 0, 33)
	request = append(request, bedrockUnconnectedPingPacketID)

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], pingTime)
	request = append(request, scratch[:]...)
	request = append(request, bedrockMagic[:]...)
	binary.BigEndian.PutUint64(scratch[:], bedrockProbeClientGUID)
	request = append(request, scratch[:]...)

	if _, err := conn.Write(request); err != nil {
		return err
	}

	var buf [2048]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return err
	}
	return parseBedrockPong(buf[:n], pingTime)
}

func parseBedrockPong(payload []byte, expectedPingTime uint64) error {
	if len(payload) < 35 {
		return fmt.Errorf("bedrock pong too short: %d", len(payload))
	}
	if payload[0] != bedrockUnconnectedPongPacketID {
		return fmt.Errorf("unexpected bedrock pong packet id: %d", payload[0])
	}
	if pingTime := binary.BigEndian.Uint64(payload[1:9]); pingTime != expectedPingTime {
		return errors.New("bedrock pong ping time mismatch")
	}
	if !slices.Equal(payload[17:33], bedrockMagic[:]) {
		return errors.New("bedrock pong magic mismatch")
	}

	statusLength := int(binary.BigEndian.Uint16(payload[33:35]))
	if len(payload) != 35+statusLength {
		return fmt.Errorf("unexpected bedrock pong length: %d", len(payload))
	}

	status := string(payload[35:])
	return validateBedrockStatus(status)
}
