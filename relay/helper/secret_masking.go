package helper

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
)

var (
	// Patterns for secrets commonly seen in developer prompts
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(sk-[a-zA-Z0-9]{20,})\b`),          // OpenAI / generic sk keys
		regexp.MustCompile(`\b(ghp_[a-zA-Z0-9]{36})\b`),           // GitHub Personal Access Token
		regexp.MustCompile(`\b(github_pat_[a-zA-Z0-9_]{40,})\b`),  // GitHub Fine-grained token
		regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`),              // AWS Access Key ID
		regexp.MustCompile(`\b(AIza[0-9A-Za-z\-_]{35})\b`),        // Google API Key
		regexp.MustCompile(`\b(xox[baprs]-[0-9a-zA-Z]{10,48})\b`), // Slack tokens
	}

	maskSessionPool sync.Map // map[string]*SecretMaskSession
)

// SecretMaskSession holds the bidirectional replacement dictionary for one request turn.
type SecretMaskSession struct {
	mu           sync.RWMutex
	secretToMask map[string]string
	maskToSecret map[string]string
}

func NewSecretMaskSession() *SecretMaskSession {
	return &SecretMaskSession{
		secretToMask: make(map[string]string),
		maskToSecret: make(map[string]string),
	}
}

// MaskSecrets replaces sensitive tokens with deterministic surrogate masks.
func (s *SecretMaskSession) MaskSecrets(text string) string {
	if text == "" {
		return text
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, re := range secretPatterns {
		text = re.ReplaceAllStringFunc(text, func(secret string) string {
			if mask, exists := s.secretToMask[secret]; exists {
				return mask
			}
			var randBytes [4]byte
			_, _ = rand.Read(randBytes[:])
			mask := "[REDACTED_SECRET_" + hex.EncodeToString(randBytes[:]) + "]"
			s.secretToMask[secret] = mask
			s.maskToSecret[mask] = secret
			return mask
		})
	}
	return text
}

// UnmaskSecrets restores original sensitive tokens in responses.
func (s *SecretMaskSession) UnmaskSecrets(text string) string {
	if text == "" || len(s.maskToSecret) == 0 {
		return text
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for mask, secret := range s.maskToSecret {
		if strings.Contains(text, mask) {
			text = strings.ReplaceAll(text, mask, secret)
		}
	}
	return text
}

// StoreMaskSession saves session for the duration of the request turn.
func StoreMaskSession(requestID string, session *SecretMaskSession) {
	if requestID != "" && session != nil {
		maskSessionPool.Store(requestID, session)
	}
}

// GetMaskSession retrieves the mask session without removing it.
func GetMaskSession(requestID string) *SecretMaskSession {
	if requestID == "" {
		return nil
	}
	val, ok := maskSessionPool.Load(requestID)
	if !ok || val == nil {
		return nil
	}
	return val.(*SecretMaskSession)
}

// GetAndRemoveMaskSession retrieves and cleans up the mask session.
func GetAndRemoveMaskSession(requestID string) *SecretMaskSession {
	if requestID == "" {
		return nil
	}
	val, ok := maskSessionPool.LoadAndDelete(requestID)
	if !ok || val == nil {
		return nil
	}
	return val.(*SecretMaskSession)
}
