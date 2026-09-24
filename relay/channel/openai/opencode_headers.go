package openai

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// openCodeUA must be a release-looking opencode version ≥ 1.17.0.
// Non-release suffixes were rejected by the free-tier gate (issue #49433).
const openCodeUA = "opencode/1.18.31 ai-sdk/provider-utils/4.0.40 runtime/bun/1.3.14"

const openCodeBase62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func newOpenCodeSessionID() string {
	return "ses_" + openCodeIDBody()
}

func newOpenCodeRequestID() string {
	return "msg_" + openCodeIDBody()
}

// openCodeIDBody builds 12 hex + 14 base62 (26 chars total) matching
// ^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$ used by the Zen gatekeeper.
func openCodeIDBody() string {
	var buf [13]byte // 6 bytes hex (12 chars) + 7 bytes → 14 base62 chars
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure is fatal for identity headers; fall back to
		// a fixed-shape id so request still matches the gate regex.
		return "000000000000" + "00000000000000"
	}
	hexPart := hex.EncodeToString(buf[:6]) // 12 hex chars
	base62 := make([]byte, 14)
	for i := 0; i < 14; i++ {
		base62[i] = openCodeBase62Alphabet[int(buf[6+i%7])%len(openCodeBase62Alphabet)]
	}
	return hexPart + string(base62)
}

func applyOpenCodeFreeTierHeaders(header *http.Header) {
	if header == nil {
		return
	}
	if header.Get("x-opencode-session") == "" {
		header.Set("x-opencode-session", newOpenCodeSessionID())
	}
	if header.Get("x-opencode-request") == "" {
		header.Set("x-opencode-request", newOpenCodeRequestID())
	}
	if header.Get("x-opencode-client") == "" {
		header.Set("x-opencode-client", "cli")
	}
	if header.Get("x-opencode-project") == "" {
		header.Set("x-opencode-project", "global")
	}
	if header.Get("User-Agent") == "" {
		header.Set("User-Agent", openCodeUA)
	}
}
