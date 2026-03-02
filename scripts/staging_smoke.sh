#!/usr/bin/env bash
set -euo pipefail

USER_AGENT="${USER_AGENT:-Better Uptime Bot Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/74.0.3729.169 Safari/537.36}"

echo "Using user-agent string for consistency: $USER_AGENT"

go test ./... \
  -count=1 \
  -run '^(TestPingServer|TestPingServerMalformedStatusPacket|TestPingServerPongMismatch|TestPingServerWithOptionsRejectsPrivateAddressByDefault|TestPingServerWithOptionsAllowsPrivateAddressWhenEnabled|TestPingServerRejectsExcessiveTimeout|TestPingServerRejectsControlCharacterInHost)$'
