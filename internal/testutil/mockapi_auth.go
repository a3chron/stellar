// mockapi_auth.go implements the mock's CLI device-login routes
// (/api/cli/auth/{start,token,approve,whoami} and DELETE .../token) from the
// API contract's "Auth" table.
package testutil

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// mockAuthOutcome configures how the device-login flow the next
// StartDeviceAuth call creates will resolve - consumed (reset to nil) by
// that call, so later flows in the same test go back to the default
// (approve on the first poll) unless reconfigured.
type mockAuthOutcome struct {
	kind              string // "approve" (default), "deny", "expire"
	approveAfterPolls int
	slowDownOnce      bool
	username, name    string
}

type mockDeviceRequest struct {
	deviceCode, userCode string
	outcome              mockAuthOutcome
	pollCount            int
	slowDownConsumed     bool
	expiresAt            time.Time
}

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// SetNextAuthApprove configures the next `stellar auth` flow to succeed as
// username/name after afterPolls polls (1 means "on the very first poll").
func (h *MockAPIHandler) SetNextAuthApprove(afterPolls int, username, name string) {
	if afterPolls < 1 {
		afterPolls = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextAuthOutcome = &mockAuthOutcome{kind: "approve", approveAfterPolls: afterPolls, username: username, name: name}
}

// SetNextAuthDeny configures the next `stellar auth` flow to be denied after
// afterPolls polls.
func (h *MockAPIHandler) SetNextAuthDeny(afterPolls int) {
	if afterPolls < 1 {
		afterPolls = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextAuthOutcome = &mockAuthOutcome{kind: "deny", approveAfterPolls: afterPolls}
}

// SetNextAuthExpired configures the next `stellar auth` flow's device code
// to already be expired by the time it's first polled.
func (h *MockAPIHandler) SetNextAuthExpired() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextAuthOutcome = &mockAuthOutcome{kind: "expire"}
}

// SetNextAuthSlowDownOnce makes the next flow's very first poll answer
// slow_down once before resolving normally (approved on the first poll by
// default - call SetNextAuthApprove first to change that).
func (h *MockAPIHandler) SetNextAuthSlowDownOnce() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.nextAuthOutcome == nil {
		h.nextAuthOutcome = &mockAuthOutcome{kind: "approve", approveAfterPolls: 1, username: "testuser", name: "Test User"}
	}
	h.nextAuthOutcome.slowDownOnce = true
}

// Token returns the bearer token for a logged-in username, registering that
// username first if needed - a shortcut for tests that want an authed
// client without driving the full device-login flow.
func (h *MockAPIHandler) Token(username, name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.users[username] = mockUserInfo{name: name}
	token := "stl_" + randomToken(24)
	h.tokens[token] = username
	return token
}

func (h *MockAPIHandler) handleCLIAuth(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) != 1 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	switch rest[0] {
	case "start":
		h.handleAuthStart(w, r)
	case "token":
		if r.Method == http.MethodDelete {
			h.handleRevokeToken(w, r)
			return
		}
		h.handleAuthToken(w, r)
	case "whoami":
		h.handleAuthWhoami(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *MockAPIHandler) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.mu.Lock()
	h.deviceSeq++
	deviceCode := fmt.Sprintf("device-%d-%s", h.deviceSeq, randomToken(8))
	userCode := fmt.Sprintf("%04d-%04d", h.deviceSeq, 1000+h.deviceSeq)

	outcome := h.nextAuthOutcome
	h.nextAuthOutcome = nil
	if outcome == nil {
		outcome = &mockAuthOutcome{kind: "approve", approveAfterPolls: 1, username: "testuser", name: "Test User"}
	}

	expiresAt := time.Now().Add(10 * time.Minute)
	if outcome.kind == "expire" {
		expiresAt = time.Now().Add(-time.Second)
	}

	h.deviceRequests[deviceCode] = &mockDeviceRequest{
		deviceCode: deviceCode,
		userCode:   userCode,
		outcome:    *outcome,
		expiresAt:  expiresAt,
	}
	h.userCodeToDevice[userCode] = deviceCode
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deviceCode":              deviceCode,
		"userCode":                userCode,
		"verificationUrl":         "http://mock.stellar-hub.test/cli/authorize",
		"verificationUrlComplete": "http://mock.stellar-hub.test/cli/authorize?code=" + userCode,
		"expiresIn":               600,
		// Per the plan: the mock returns interval:0 so authSleep (which
		// tests stub out) is never what makes a test slow.
		"interval": 0,
	})
}

// SetNextAuthServerErrors makes the next n POST /api/cli/auth/token requests
// (for any device code) answer 500 with a plain-text, non-JSON body -
// mirroring a Vercel gateway error during a hub deploy - before falling
// through to the normal flow. Used to test that polling survives a
// transient 5xx instead of giving up on the first one.
func (h *MockAPIHandler) SetNextAuthServerErrors(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authTokenForcedErrors = n
}

func (h *MockAPIHandler) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.mu.Lock()
	if h.authTokenForcedErrors > 0 {
		h.authTokenForcedErrors--
		h.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
		return
	}
	h.mu.Unlock()

	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceCode == "" {
		writeJSONError(w, http.StatusBadRequest, "validation_failed", "deviceCode is required", nil, nil)
		return
	}

	h.mu.Lock()
	req, ok := h.deviceRequests[body.DeviceCode]
	if !ok {
		h.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "invalid code", nil, nil)
		return
	}

	if time.Now().After(req.expiresAt) {
		delete(h.deviceRequests, req.deviceCode)
		delete(h.userCodeToDevice, req.userCode)
		h.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "expired_token", "code expired", nil, nil)
		return
	}

	if req.outcome.slowDownOnce && !req.slowDownConsumed {
		req.slowDownConsumed = true
		h.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "slow_down", "slow down", nil, nil)
		return
	}

	req.pollCount++
	threshold := req.outcome.approveAfterPolls
	if threshold < 1 {
		threshold = 1
	}

	if req.pollCount < threshold {
		h.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "authorization_pending", "pending", nil, nil)
		return
	}

	switch req.outcome.kind {
	case "deny":
		delete(h.deviceRequests, req.deviceCode)
		delete(h.userCodeToDevice, req.userCode)
		h.mu.Unlock()
		writeJSONError(w, http.StatusBadRequest, "access_denied", "denied", nil, nil)
	default: // "approve"
		username, name := req.outcome.username, req.outcome.name
		if username == "" {
			username = "testuser"
		}
		if name == "" {
			name = "Test User"
		}
		h.users[username] = mockUserInfo{name: name}
		token := "stl_" + randomToken(24)
		h.tokens[token] = username
		delete(h.deviceRequests, req.deviceCode)
		delete(h.userCodeToDevice, req.userCode)
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token": token,
			"user":  map[string]string{"username": username, "name": name},
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header, or "" if absent/malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

// authenticate resolves the bearer token on r to a username, writing the
// contract's 401 invalid_token and returning ok=false if it's missing or
// unknown.
func (h *MockAPIHandler) authenticate(w http.ResponseWriter, r *http.Request) (username string, ok bool) {
	token := bearerToken(r)
	if token == "" {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "missing or invalid token", nil, nil)
		return "", false
	}

	h.mu.Lock()
	username, found := h.tokens[token]
	h.mu.Unlock()
	if !found {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "missing or invalid token", nil, nil)
		return "", false
	}
	return username, true
}

func (h *MockAPIHandler) handleAuthWhoami(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	h.mu.Lock()
	info := h.users[username]
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username": username,
		"name":     info.name,
		"image":    info.image,
	})
}

// handleRevokeToken implements DELETE /api/cli/auth/token.
func (h *MockAPIHandler) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := bearerToken(r)
	h.mu.Lock()
	_, found := h.tokens[token]
	if found {
		delete(h.tokens, token)
	}
	h.mu.Unlock()

	if !found {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "missing or invalid token", nil, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string, missing []string, details interface{}) {
	writeJSON(w, status, map[string]interface{}{
		"error":   message,
		"code":    code,
		"missing": missing,
		"details": details,
	})
}
