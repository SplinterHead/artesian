package epic

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"artesian/internal/db"
	"artesian/internal/ports"
	"artesian/internal/ws"
)

const (
	LauncherClientID     = "34a02cf8f4414e29b15921876da36f9a"
	LauncherClientSecret = "daafbccc737745039dffe53d94fc76cf"
	LauncherBasicAuth    = "Basic MzRhMDJjZjhmNDQxNGUyOWIxNTkyMTg3NmRhMzZmOWE6ZGFhZmJjY2M3Mzc3NDUwMzlkZmZlNTNkOTRmYzc2Y2Y="
	TokenEndpoint        = "https://account-public-service-prod03.ol.epicgames.com/account/api/oauth/token"
	LoginURL             = "https://www.epicgames.com/id/login?redirectUrl=https%3A%2F%2Fwww.epicgames.com%2Fid%2Fapi%2Fredirect%3FclientId%3D34a02cf8f4414e29b15921876da36f9a%26responseType%3Dcode"
)

type oauthTokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	ExpiresAt        string `json:"expires_at"`
	TokenType        string `json:"token_type"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
	AccountID        string `json:"account_id"`
	ClientID         string `json:"client_id"`
	DisplayName      string `json:"displayName"`
	ErrorMessage     string `json:"errorMessage,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
}

// Status returns current Epic Games authentication status
func (a *Adapter) Status() ports.AuthStatus {
	a.authMu.RLock()
	defer a.authMu.RUnlock()

	return ports.AuthStatus{
		State:        a.state,
		Username:     a.username,
		AccountID:    a.accountID,
		TokenExpiry:  a.tokenExpiry,
		HasValidAuth: a.state == ports.StateConnected && a.accessToken != "",
		ErrorMessage: a.errorMessage,
	}
}

// GetAccessToken returns the active OAuth access token, refreshing it if nearing expiration
func (a *Adapter) GetAccessToken() string {
	a.authMu.Lock()
	defer a.authMu.Unlock()

	if a.refreshToken != "" && time.Now().After(a.tokenExpiry.Add(-5*time.Minute)) {
		_ = a.refreshAccessTokenLocked()
	}
	return a.accessToken
}

// GetAccountID returns the authenticated Epic account ID
func (a *Adapter) GetAccountID() string {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.accountID
}

// ExtractAuthorizationCode parses an authorization code from raw hex, JSON payloads, or redirect URLs
func ExtractAuthorizationCode(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	// 1. Check if user pasted JSON payload e.g. {"authorizationCode": "..."}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var jsonPayload struct {
			AuthorizationCode string `json:"authorizationCode"`
			Code              string `json:"code"`
		}
		if err := json.Unmarshal([]byte(trimmed), &jsonPayload); err == nil {
			if jsonPayload.AuthorizationCode != "" {
				return jsonPayload.AuthorizationCode
			}
			if jsonPayload.Code != "" {
				return jsonPayload.Code
			}
		}
	}

	// 2. Check if user pasted full redirect URL e.g. https://...?code=...
	if strings.Contains(trimmed, "code=") {
		if parsedURL, err := url.Parse(trimmed); err == nil {
			if code := parsedURL.Query().Get("code"); code != "" {
				return code
			}
			if code := parsedURL.Query().Get("authorizationCode"); code != "" {
				return code
			}
		}
	}

	// 3. Fallback: extract 32-character alphanumeric code
	re := regexp.MustCompile(`[0-9a-fA-F]{32}`)
	if match := re.FindString(trimmed); match != "" {
		return match
	}

	return trimmed
}

// Login authenticates using the Epic Games authorization code
func (a *Adapter) Login(codeOrInput, password string) error {
	authCode := ExtractAuthorizationCode(codeOrInput)
	if authCode == "" {
		return fmt.Errorf("invalid authorization code provided")
	}

	a.authMu.Lock()
	a.state = ports.StateConnecting
	a.errorMessage = ""
	a.authMu.Unlock()
	a.broadcastStatus()

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("code", authCode)
	formData.Set("token_type", "eg1")

	req, err := http.NewRequest("POST", TokenEndpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		a.failLogin(fmt.Sprintf("Failed to build request: %v", err))
		return err
	}

	req.Header.Set("Authorization", LauncherBasicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.failLogin(fmt.Sprintf("Network connection error: %v", err))
		return err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		a.failLogin(fmt.Sprintf("Invalid response from Epic Games: %v", err))
		return err
	}

	if resp.StatusCode != http.StatusOK || tokenResp.AccessToken == "" {
		errMsg := tokenResp.ErrorMessage
		if errMsg == "" {
			errMsg = fmt.Sprintf("Authentication failed with HTTP %d", resp.StatusCode)
		}
		a.failLogin(errMsg)
		return fmt.Errorf(errMsg)
	}

	// Successful authorization!
	username := tokenResp.DisplayName
	if username == "" {
		username = "Epic User"
	}

	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	a.authMu.Lock()
	a.state = ports.StateConnected
	a.username = username
	a.accountID = tokenResp.AccountID
	a.accessToken = tokenResp.AccessToken
	a.refreshToken = tokenResp.RefreshToken
	a.tokenExpiry = expiry
	a.errorMessage = ""
	a.authMu.Unlock()

	// Persist session in SQLite
	if a.db != nil {
		sess := &db.AuthSession{
			Service:      a.ID(),
			Username:     username,
			AccountID:    tokenResp.AccountID,
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ClientID:     LauncherClientID,
			IsActive:     true,
			UpdatedAt:    time.Now(),
		}
		_ = a.db.SaveAuthSession(sess)
		_ = a.db.SetServiceAutoUpdate(a.ID(), true)
	}

	log.Printf("[Epic Auth] Authentication successful! Established session for %s (Account ID: %s)", username, tokenResp.AccountID)
	a.broadcastStatus()
	return nil
}

// refreshAccessTokenLocked refreshes the access token using the stored refresh_token (caller must hold authMu)
func (a *Adapter) refreshAccessTokenLocked() error {
	if a.refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("refresh_token", a.refreshToken)
	formData.Set("token_type", "eg1")

	req, err := http.NewRequest("POST", TokenEndpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", LauncherBasicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK || tokenResp.AccessToken == "" {
		log.Printf("[Epic Auth] Refresh token expired or rejected: %s", tokenResp.ErrorMessage)
		a.state = ports.StateDisconnected
		a.errorMessage = "Session expired. Please reconnect your Epic Games account."
		if a.db != nil {
			_ = a.db.ClearAuthSession(a.ID())
		}
		return fmt.Errorf("session expired: %s", tokenResp.ErrorMessage)
	}

	a.accessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		a.refreshToken = tokenResp.RefreshToken
	}
	a.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.state = ports.StateConnected

	if a.db != nil {
		sess := &db.AuthSession{
			Service:      a.ID(),
			Username:     a.username,
			AccountID:    a.accountID,
			AccessToken:  a.accessToken,
			RefreshToken: a.refreshToken,
			ClientID:     LauncherClientID,
			IsActive:     true,
			UpdatedAt:    time.Now(),
		}
		_ = a.db.SaveAuthSession(sess)
	}

	log.Printf("[Epic Auth] Refreshed access token successfully for %s", a.username)
	return nil
}

func (a *Adapter) restoreSavedSession() {
	if a.db == nil {
		return
	}

	sess, err := a.db.GetAuthSession(a.ID())
	if err != nil || sess == nil || !sess.IsActive || sess.RefreshToken == "" {
		return
	}

	a.authMu.Lock()
	a.username = sess.Username
	a.accountID = sess.AccountID
	a.refreshToken = sess.RefreshToken
	a.accessToken = sess.AccessToken
	a.state = ports.StateConnecting
	a.authMu.Unlock()

	// Refresh token in background to verify active session
	go func() {
		a.authMu.Lock()
		err := a.refreshAccessTokenLocked()
		username := a.username
		accountID := a.accountID
		a.authMu.Unlock()

		if err != nil {
			log.Printf("[Epic Auth] Could not restore saved session: %v", err)
		} else {
			log.Printf("[Epic Auth] Restored genuine Epic Games session for %s (Account ID: %s)", username, accountID)
		}
		a.broadcastStatus()
	}()
}

func (a *Adapter) failLogin(msg string) {
	log.Printf("[Epic Auth] Login error: %s", msg)
	a.authMu.Lock()
	a.state = ports.StateError
	a.errorMessage = msg
	a.authMu.Unlock()
	a.broadcastStatus()
}

// Logout clears stored Epic Games credentials and marks session inactive
func (a *Adapter) Logout() error {
	a.authMu.Lock()
	username := a.username
	a.state = ports.StateDisconnected
	a.username = ""
	a.accountID = ""
	a.accessToken = ""
	a.refreshToken = ""
	a.errorMessage = ""
	a.authMu.Unlock()

	if a.db != nil {
		_ = a.db.ClearAuthSession(a.ID())
	}

	log.Printf("[Epic Auth] Session cleared for %s", username)
	a.broadcastStatus()
	return nil
}

func (a *Adapter) Submit2FA(code string) error {
	return nil
}

func (a *Adapter) Cancel2FA() error {
	return nil
}

func (a *Adapter) broadcastStatus() {
	ws.GetHub().Broadcast("epic:status", a.Status())
}
