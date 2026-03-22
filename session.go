package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/netip"
	"time"
)

type probeSample struct {
	latency time.Duration
	remote  netip.AddrPort
}

type preparedProbe interface {
	banner(numeric bool) string
	summaryLabel(numeric bool) string
	probe(ctx context.Context, timeout time.Duration) (probeSample, error)
}

type sleepFunc func(context.Context, time.Duration) error

type sessionRuntime struct {
	now   func() time.Time
	sleep sleepFunc
}

func defaultSessionRuntime() sessionRuntime {
	return sessionRuntime{
		now: time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			if d <= 0 {
				return nil
			}
			timer := time.NewTimer(d)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

type sessionStats struct {
	transmitted int
	received    int
	start       time.Time
	end         time.Time
	rtt         rttStats
}

type rttStats struct {
	count int
	min   float64
	max   float64
	mean  float64
	m2    float64
}

func (s *rttStats) add(d time.Duration) {
	ms := float64(d) / float64(time.Millisecond)
	s.count++
	if s.count == 1 {
		s.min = ms
		s.max = ms
		s.mean = ms
		return
	}
	if ms < s.min {
		s.min = ms
	}
	if ms > s.max {
		s.max = ms
	}
	delta := ms - s.mean
	s.mean += delta / float64(s.count)
	s.m2 += delta * (ms - s.mean)
}

func (s rttStats) mdev() float64 {
	if s.count == 0 {
		return 0
	}
	return math.Sqrt(s.m2 / float64(s.count))
}

func runTextSession(ctx context.Context, stdout io.Writer, cfg cliConfig, probe preparedProbe, rt sessionRuntime) int {
	if rt.now == nil {
		rt.now = time.Now
	}
	if rt.sleep == nil {
		rt = defaultSessionRuntime()
	}

	stats := sessionStats{start: rt.now()}
	sessionStart := stats.start
	writeSessionLine(stdout, false, rt.now, "%s", probe.banner(cfg.Numeric))

	var (
		deadlineAt  time.Time
		hasDeadline bool
	)
	if cfg.Deadline > 0 {
		deadlineAt = sessionStart.Add(cfg.Deadline)
		hasDeadline = true
	}

	seq := 1
	for {
		if cfg.Count > 0 && stats.transmitted >= cfg.Count {
			break
		}
		if hasDeadline && !rt.now().Before(deadlineAt) {
			break
		}

		probeStart := rt.now()
		timeout := cfg.Timeout
		if hasDeadline {
			remaining := deadlineAt.Sub(rt.now())
			if remaining <= 0 {
				break
			}
			if remaining < timeout {
				timeout = remaining
			}
		}

		stats.transmitted++
		sample, err := probe.probe(ctx, timeout)
		if err == nil {
			stats.received++
			stats.rtt.add(sample.latency)
			if !cfg.Quiet {
				writeSessionLine(
					stdout,
					cfg.Timestamp,
					rt.now,
					"pong from %s: seq=%d time=%.3f ms",
					formatAddrPort(sample.remote),
					seq,
					float64(sample.latency)/float64(time.Millisecond),
				)
			}
		}
		seq++

		if ctx.Err() != nil {
			break
		}
		if cfg.Count > 0 && stats.transmitted >= cfg.Count {
			break
		}
		if hasDeadline && !rt.now().Before(deadlineAt) {
			break
		}

		sleepFor := cfg.Interval - rt.now().Sub(probeStart)
		if sleepFor < 0 {
			sleepFor = 0
		}
		if err := rt.sleep(ctx, sleepFor); err != nil {
			break
		}
	}

	stats.end = rt.now()
	_, _ = fmt.Fprintln(stdout)
	writeSessionLine(stdout, false, rt.now, "--- %s ping statistics ---", probe.summaryLabel(cfg.Numeric))
	writeSessionLine(
		stdout,
		false,
		rt.now,
		"%d probes transmitted, %d received, %.0f%% packet loss, time %dms",
		stats.transmitted,
		stats.received,
		packetLossPercent(stats.transmitted, stats.received),
		stats.end.Sub(stats.start).Milliseconds(),
	)
	if stats.rtt.count > 0 {
		writeSessionLine(
			stdout,
			false,
			rt.now,
			"rtt min/avg/max/mdev = %.3f/%.3f/%.3f/%.3f ms",
			stats.rtt.min,
			stats.rtt.mean,
			stats.rtt.max,
			stats.rtt.mdev(),
		)
	}

	if cfg.Count > 0 && cfg.Deadline > 0 && stats.received < cfg.Count {
		return 1
	}
	if stats.received == 0 {
		return 1
	}
	return 0
}

func packetLossPercent(transmitted int, received int) float64 {
	if transmitted == 0 {
		return 0
	}
	lost := transmitted - received
	return float64(lost) * 100 / float64(transmitted)
}

func writeSessionLine(w io.Writer, withTimestamp bool, now func() time.Time, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if withTimestamp {
		t := now()
		secs := t.Unix()
		micros := t.Nanosecond() / 1000
		line = fmt.Sprintf("[%d.%06d] %s", secs, micros, line)
	}
	_, _ = fmt.Fprintln(w, line)
}

func formatAddrPort(addr netip.AddrPort) string {
	if !addr.IsValid() {
		return "unknown"
	}
	return addr.String()
}
