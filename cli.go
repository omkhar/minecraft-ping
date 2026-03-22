package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type pingResult struct {
	Server    string `json:"server"`
	LatencyMs int    `json:"latency_ms"`
}

type cliConfig struct {
	Target      targetSpec
	Edition     edition
	Options     pingOptions
	Count       int
	Interval    time.Duration
	Deadline    time.Duration
	Timeout     time.Duration
	Quiet       bool
	Timestamp   bool
	Numeric     bool
	JSON        bool
	ShowHelp    bool
	ShowVersion bool
}

type parseStatus uint8

const (
	parseStatusOK parseStatus = iota
	parseStatusHelp
	parseStatusInvalid
)

type rawCLIConfig struct {
	destination  string
	edition      string
	editionSet   bool
	javaAlias    bool
	bedrockAlias bool
	forceIPv4    bool
	forceIPv6    bool
	count        string
	interval     string
	deadline     string
	timeout      string
	quiet        bool
	timestamp    bool
	numeric      bool
	json         bool
	showHelp     bool
	showVersion  bool
}

type cliRuntime struct {
	newContext func() (context.Context, context.CancelFunc)
	session    sessionRuntime
	prepare    func(context.Context, cliConfig) (preparedProbe, error)
}

func defaultCLIRuntime() cliRuntime {
	return cliRuntime{
		newContext: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		},
		session: defaultSessionRuntime(),
		prepare: prepareProbe,
	}
}

func usageText() string {
	return strings.TrimSpace(`
Usage: minecraft-ping [options] destination

Options:
  -4                    use IPv4 only
  -6                    use IPv6 only
  -c count              stop after count probes
  -i interval           wait interval seconds between probes
  -w deadline           stop after deadline seconds
  -W timeout            wait timeout seconds for each probe
  -q                    quiet output
  -D                    print unix timestamp before each output line
  -n                    numeric output only
  -j                    JSON output (single probe)
  -V, --version         print version and exit
  -h, --help            show help
  --edition kind        java or bedrock
  --java                alias for --edition java
  --bedrock             alias for --edition bedrock
`)
}

func parseCLIConfig(args []string) (cliConfig, parseStatus) {
	raw, status := scanArgv(args)
	if status != parseStatusOK {
		return cliConfig{}, status
	}
	return normalizeCLIConfig(raw)
}

func scanArgv(args []string) (rawCLIConfig, parseStatus) {
	var raw rawCLIConfig

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			return raw, parseStatusInvalid
		}
		if arg == "--" {
			if i+1 >= len(args) || raw.destination != "" {
				return raw, parseStatusInvalid
			}
			raw.destination = args[i+1]
			i++
			if i+1 != len(args) {
				return raw, parseStatusInvalid
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if raw.destination != "" {
				return raw, parseStatusInvalid
			}
			raw.destination = arg
			continue
		}
		if strings.HasPrefix(arg, "--") {
			next, ok := consumeLongFlag(raw, arg, args, &i)
			if !ok {
				return raw, parseStatusInvalid
			}
			raw = next
			if raw.showHelp || raw.showVersion {
				break
			}
			continue
		}
		next, ok := consumeShortFlags(raw, arg, args, &i)
		if !ok {
			return raw, parseStatusInvalid
		}
		raw = next
		if raw.showHelp || raw.showVersion {
			break
		}
	}

	if raw.showHelp {
		return raw, parseStatusHelp
	}
	return raw, parseStatusOK
}

func consumeLongFlag(raw rawCLIConfig, arg string, args []string, index *int) (rawCLIConfig, bool) {
	name := arg
	value := ""
	hasInlineValue := false
	if eq := strings.IndexRune(arg, '='); eq >= 0 {
		name = arg[:eq]
		value = arg[eq+1:]
		hasInlineValue = true
	}

	switch name {
	case "--edition":
		if hasInlineValue && value == "" {
			return raw, false
		}
		if !hasInlineValue {
			if *index+1 >= len(args) {
				return raw, false
			}
			*index++
			value = args[*index]
		}
		if strings.TrimSpace(value) == "" {
			return raw, false
		}
		raw.edition = value
		raw.editionSet = true
		return raw, true
	case "--java":
		if hasInlineValue {
			return raw, false
		}
		raw.javaAlias = true
		return raw, true
	case "--bedrock":
		if hasInlineValue {
			return raw, false
		}
		raw.bedrockAlias = true
		return raw, true
	case "--help":
		if hasInlineValue {
			return raw, false
		}
		raw.showHelp = true
		return raw, true
	case "--version":
		if hasInlineValue {
			return raw, false
		}
		raw.showVersion = true
		return raw, true
	default:
		return raw, false
	}
}

func consumeShortFlags(raw rawCLIConfig, arg string, args []string, index *int) (rawCLIConfig, bool) {
	for pos := 1; pos < len(arg); pos++ {
		switch arg[pos] {
		case '4':
			raw.forceIPv4 = true
		case '6':
			raw.forceIPv6 = true
		case 'q':
			raw.quiet = true
		case 'D':
			raw.timestamp = true
		case 'n':
			raw.numeric = true
		case 'j':
			raw.json = true
		case 'V':
			raw.showVersion = true
		case 'h':
			raw.showHelp = true
		case 'c', 'i', 'w', 'W':
			value := arg[pos+1:]
			if value == "" {
				if *index+1 >= len(args) {
					return raw, false
				}
				*index++
				value = args[*index]
			}
			switch arg[pos] {
			case 'c':
				raw.count = value
			case 'i':
				raw.interval = value
			case 'w':
				raw.deadline = value
			case 'W':
				raw.timeout = value
			}
			return raw, true
		default:
			return raw, false
		}
	}
	return raw, true
}

func normalizeCLIConfig(raw rawCLIConfig) (cliConfig, parseStatus) {
	cfg := cliConfig{
		Edition:  editionJava,
		Options:  pingOptions{addressFamily: addressFamilyAny},
		Interval: time.Second,
		Timeout:  5 * time.Second,
	}

	if raw.showVersion {
		cfg.ShowVersion = true
		return cfg, parseStatusOK
	}

	if raw.forceIPv4 && raw.forceIPv6 {
		return cliConfig{}, parseStatusInvalid
	}
	switch {
	case raw.forceIPv4:
		cfg.Options.addressFamily = addressFamily4
	case raw.forceIPv6:
		cfg.Options.addressFamily = addressFamily6
	}

	editionSetCount := 0
	if raw.editionSet {
		editionSetCount++
	}
	if raw.javaAlias {
		editionSetCount++
	}
	if raw.bedrockAlias {
		editionSetCount++
	}
	if editionSetCount > 1 {
		return cliConfig{}, parseStatusInvalid
	}
	switch {
	case raw.javaAlias:
		cfg.Edition = editionJava
	case raw.bedrockAlias:
		cfg.Edition = editionBedrock
	case raw.editionSet:
		editionValue, err := parseEdition(raw.edition)
		if err != nil {
			return cliConfig{}, parseStatusInvalid
		}
		cfg.Edition = editionValue
	}

	if raw.destination == "" {
		return cliConfig{}, parseStatusInvalid
	}
	target, err := parseDestination(raw.destination)
	if err != nil {
		return cliConfig{}, parseStatusInvalid
	}
	if err := target.validate(); err != nil {
		return cliConfig{}, parseStatusInvalid
	}
	cfg.Target = target

	if raw.count != "" {
		count, err := strconv.Atoi(raw.count)
		if err != nil || count <= 0 {
			return cliConfig{}, parseStatusInvalid
		}
		cfg.Count = count
	}
	if raw.interval != "" {
		interval, ok := parseSecondsDuration(raw.interval)
		if !ok {
			return cliConfig{}, parseStatusInvalid
		}
		cfg.Interval = interval
	}
	if raw.deadline != "" {
		deadline, ok := parseSecondsDuration(raw.deadline)
		if !ok {
			return cliConfig{}, parseStatusInvalid
		}
		cfg.Deadline = deadline
	}
	if raw.timeout != "" {
		timeout, ok := parseSecondsDuration(raw.timeout)
		if !ok || timeout > maxAllowedTimeout {
			return cliConfig{}, parseStatusInvalid
		}
		cfg.Timeout = timeout
	}

	cfg.Quiet = raw.quiet
	cfg.Timestamp = raw.timestamp
	cfg.Numeric = raw.numeric
	cfg.JSON = raw.json

	if cfg.JSON && (cfg.Count > 0 || raw.interval != "" || raw.deadline != "" || cfg.Quiet || cfg.Timestamp) {
		return cliConfig{}, parseStatusInvalid
	}

	return cfg, parseStatusOK
}

func parseSecondsDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}

	seconds, ok := new(big.Rat).SetString(raw)
	if !ok || seconds.Sign() <= 0 {
		return 0, false
	}

	nanoseconds := new(big.Rat).Mul(seconds, big.NewRat(int64(time.Second), 1))
	if nanoseconds.Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
		return 0, false
	}
	duration := new(big.Int).Quo(nanoseconds.Num(), nanoseconds.Denom())
	if duration.Sign() <= 0 || !duration.IsInt64() {
		return 0, false
	}

	return time.Duration(duration.Int64()), true
}

func durationToLatencyMs(d time.Duration) int {
	latencyMs := int(d / time.Millisecond)
	if latencyMs < 1 {
		return 1
	}
	return latencyMs
}

func run(argv []string, stdout io.Writer, stderr io.Writer) int {
	return runWithRuntime(argv, stdout, stderr, defaultCLIRuntime())
}

func runWithRuntime(argv []string, stdout io.Writer, stderr io.Writer, rt cliRuntime) int {
	if len(argv) > 1 {
		argv = argv[1:]
	} else {
		argv = nil
	}

	cfg, status := parseCLIConfig(argv)
	switch status {
	case parseStatusHelp:
		_, _ = io.WriteString(stdout, usageText()+"\n")
		return 0
	case parseStatusInvalid:
		_, _ = io.WriteString(stdout, usageText()+"\n")
		return 2
	}

	if cfg.ShowVersion {
		_, _ = io.WriteString(stdout, versionLine()+"\n")
		return 0
	}

	if rt.newContext == nil {
		rt.newContext = defaultCLIRuntime().newContext
	}
	if rt.prepare == nil {
		rt.prepare = prepareProbe
	}
	if rt.session.now == nil || rt.session.sleep == nil {
		rt.session = defaultSessionRuntime()
	}

	ctx, cancel := rt.newContext()
	defer cancel()

	probe, err := rt.prepare(ctx, cfg)
	if err != nil {
		_, _ = io.WriteString(stderr, err.Error()+"\n")
		return 2
	}

	if cfg.JSON {
		sample, err := probe.probe(ctx, cfg.Timeout)
		if err != nil {
			_, _ = io.WriteString(stderr, err.Error()+"\n")
			return 1
		}

		payload, _ := json.Marshal(pingResult{
			Server:    cfg.Target.Host,
			LatencyMs: durationToLatencyMs(sample.latency),
		})
		_, _ = io.WriteString(stdout, string(payload)+"\n")
		return 0
	}

	return runTextSession(ctx, stdout, cfg, probe, rt.session)
}
