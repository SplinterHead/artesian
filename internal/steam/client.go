package steam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"artesian/internal/db"
	"artesian/internal/ws"
)

// AuthState describes the Steam login status
type AuthState string

const (
	StateDisconnected AuthState = "disconnected"
	StateConnecting   AuthState = "connecting"
	StateAwaiting2FA  AuthState = "awaiting_2fa"
	StateConnected    AuthState = "connected"
	StateError        AuthState = "error"
)

// SessionStatus represents public Steam session info for the UI
type SessionStatus struct {
	State                  AuthState `json:"state"`
	Username               string    `json:"username"`
	SteamID                string    `json:"steam_id,omitempty"`
	ConnectedAt            time.Time `json:"connected_at,omitempty"`
	TokenExpiry            time.Time `json:"token_expiry,omitempty"`
	HasValidAuth           bool      `json:"has_valid_auth"`
	ErrorMessage           string    `json:"error_message,omitempty"`
	HasMobileConfirmation bool      `json:"has_mobile_confirmation"`
	HasDeviceCode         bool      `json:"has_device_code"`
	HasEmailCode          bool      `json:"has_email_code"`
	AssociatedMessage     string    `json:"associated_message,omitempty"`
}

// Client manages the connection and session to Steam
type Client struct {
	db                     *db.DB
	httpClient             *http.Client
	state                  AuthState
	username               string
	password               string
	clientID               string
	requestID              string
	steamID                string
	accessToken            string
	refreshToken           string
	sessionCookies         string
	confirmationType       int
	hasMobileConfirmation bool
	hasDeviceCode         bool
	hasEmailCode          bool
	associatedMsg         string
	cancelPoll             context.CancelFunc
	errorMessage           string
	mu                     sync.RWMutex
}

var (
	globalClient *Client
	clientOnce   sync.Once
)

// GetClient returns the singleton Steam client instance
func GetClient(database *db.DB) *Client {
	clientOnce.Do(func() {
		globalClient = &Client{
			db:         database,
			httpClient: &http.Client{Timeout: 20 * time.Second},
			state:      StateDisconnected,
		}
		globalClient.restoreSavedSession()
	})
	return globalClient
}

func (c *Client) restoreSavedSession() {
	if c.db == nil {
		return
	}
	sess, err := c.db.GetSteamSession()
	if err != nil {
		log.Printf("[Steam Auth] No active Steam session found in database (%v)", err)
		return
	}
	if sess != nil && sess.IsActive && sess.AccessToken != "" {

		c.mu.Lock()
		c.username = sess.Username
		c.steamID = sess.SteamID
		c.accessToken = sess.AccessToken
		c.refreshToken = sess.RefreshToken
		c.sessionCookies = sess.SessionCookies
		c.clientID = sess.ClientID
		c.state = StateConnected
		c.mu.Unlock()
		log.Printf("[Steam Auth] Restored genuine Steam session for %s (SteamID: %s)", sess.Username, sess.SteamID)
	}
}

// Status returns current Steam connectivity info
func (c *Client) Status() SessionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return SessionStatus{
		State:                 c.state,
		Username:              c.username,
		SteamID:               c.steamID,
		HasValidAuth:          c.state == StateConnected,
		ErrorMessage:          c.errorMessage,
		HasMobileConfirmation: c.hasMobileConfirmation,
		HasDeviceCode:         c.hasDeviceCode,
		HasEmailCode:          c.hasEmailCode,
		AssociatedMessage:     c.associatedMsg,
	}
}

// GetSessionCookies returns stored steam cookies (e.g. steamLoginSecure)
func (c *Client) GetSessionCookies() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionCookies
}

// GetAccessToken returns authenticated OAuth access token
func (c *Client) GetAccessToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken
}

// GetSteamID returns resolved 64-bit Steam ID
func (c *Client) GetSteamID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.steamID
}

// Login begins genuine modern Steam authentication via IAuthenticationService
func (c *Client) Login(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("username and password are required")
	}

	c.mu.Lock()
	if c.cancelPoll != nil {
		c.cancelPoll()
		c.cancelPoll = nil
	}
	c.username = username
	c.password = password
	c.state = StateConnecting
	c.errorMessage = ""
	c.hasMobileConfirmation = false
	c.hasDeviceCode = false
	c.hasEmailCode = false
	c.associatedMsg = ""
	c.mu.Unlock()

	c.broadcastStatus()
	log.Printf("[Steam Auth] Initiating genuine Steam authentication for account %s...", username)

	go c.performLogin(username, password)
	return nil
}

func (c *Client) performLogin(username, password string) {
	// 1. Fetch RSA public key for password encryption
	rsaURL := fmt.Sprintf("https://api.steampowered.com/IAuthenticationService/GetPasswordRSAPublicKey/v1/?account_name=%s", url.QueryEscape(username))
	req, err := http.NewRequest("GET", rsaURL, nil)
	if err != nil {
		c.failLogin(fmt.Sprintf("Failed to build RSA request: %v", err))
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.failLogin(fmt.Sprintf("Network error connecting to Steam RSA endpoint: %v", err))
		return
	}
	defer resp.Body.Close()

	var rsaData struct {
		Response struct {
			PublicKeyMod string `json:"publickey_mod"`
			PublicKeyExp string `json:"publickey_exp"`
			Timestamp    string `json:"timestamp"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rsaData); err != nil {
		c.failLogin(fmt.Sprintf("Failed to decode RSA response: %v", err))
		return
	}

	if rsaData.Response.PublicKeyMod == "" || rsaData.Response.PublicKeyExp == "" {
		c.failLogin("Steam returned empty RSA public key parameters")
		return
	}

	// 2. Encrypt password with RSA PKCS#1 v1.5
	encPass, err := encryptPasswordRSA(password, rsaData.Response.PublicKeyMod, rsaData.Response.PublicKeyExp)
	if err != nil {
		c.failLogin(fmt.Sprintf("Password encryption error: %v", err))
		return
	}

	// 3. BeginAuthSessionViaCredentials
	form := url.Values{}
	form.Set("account_name", username)
	form.Set("encrypted_password", encPass)
	form.Set("encryption_timestamp", rsaData.Response.Timestamp)
	form.Set("remember_login", "true")
	form.Set("platform_type", "1")
	form.Set("persistence", "1")
	form.Set("website_id", "Community")
	form.Set("device_friendly_name", "Artesian Cache")

	beginReq, err := http.NewRequest("POST", "https://api.steampowered.com/IAuthenticationService/BeginAuthSessionViaCredentials/v1/", strings.NewReader(form.Encode()))
	if err != nil {
		c.failLogin(fmt.Sprintf("Failed to build BeginAuth request: %v", err))
		return
	}
	beginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	beginReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	beginResp, err := c.httpClient.Do(beginReq)
	if err != nil {
		c.failLogin(fmt.Sprintf("Error starting Steam auth session: %v", err))
		return
	}
	defer beginResp.Body.Close()

	eresult := beginResp.Header.Get("X-eresult")
	if eresult == "5" {
		c.failLogin("Invalid Steam account name or password")
		return
	} else if eresult == "84" {
		c.failLogin("Steam rate limit exceeded; please wait a moment and try again")
		return
	} else if eresult != "" && eresult != "1" {
		c.failLogin(fmt.Sprintf("Steam authentication failed (EResult %s)", eresult))
		return
	}

	var beginData struct {
		Response struct {
			ClientID             string  `json:"client_id"`
			RequestID            string  `json:"request_id"`
			Interval             float64 `json:"interval"`
			AllowedConfirmations []struct {
				ConfirmationType int    `json:"confirmation_type"`
				AssociatedMsg    string `json:"associated_message"`
			} `json:"allowed_confirmations"`
			SteamID              string `json:"steamid"`
			WeakToken            string `json:"weak_token"`
			ExtendedErrorMessage string `json:"extended_error_message"`
		} `json:"response"`
	}
	if err := json.NewDecoder(beginResp.Body).Decode(&beginData); err != nil {
		c.failLogin(fmt.Sprintf("Failed to parse BeginAuth response: %v", err))
		return
	}

	if beginData.Response.ClientID == "" {
		errMsg := beginData.Response.ExtendedErrorMessage
		if errMsg == "" {
			errMsg = "Steam authentication session could not be established"
		}
		c.failLogin(errMsg)
		return
	}

	hasMobile := false
	hasDevice := false
	hasEmail := false
	associatedMsg := ""

	for _, conf := range beginData.Response.AllowedConfirmations {
		if conf.AssociatedMsg != "" && associatedMsg == "" {
			associatedMsg = conf.AssociatedMsg
		}
		switch conf.ConfirmationType {
		case 1: // k_EAuthSessionGuardType_AppConfirmation (Push notification Approve/Deny)
			hasMobile = true
		case 2: // k_EAuthSessionGuardType_EmailCode
			hasEmail = true
		case 3: // k_EAuthSessionGuardType_DeviceCode (Mobile authenticator 5-char code)
			hasDevice = true
		}
	}

	// Code type for manual code submission fallback:
	// If the user submits a code manually, default to 3 (DeviceCode) if mobile is enabled;
	// use 2 (EmailCode) only if strictly an email code. NEVER send 1.
	confType := 3
	if hasEmail && !hasDevice && !hasMobile {
		confType = 2
	}

	c.mu.Lock()
	c.clientID = beginData.Response.ClientID
	c.requestID = beginData.Response.RequestID
	c.steamID = beginData.Response.SteamID
	c.confirmationType = confType
	c.hasMobileConfirmation = hasMobile
	c.hasDeviceCode = hasDevice
	c.hasEmailCode = hasEmail
	c.associatedMsg = associatedMsg
	c.mu.Unlock()

	// If Steam requests a 2FA challenge (Mobile Authenticator push, TOTP, or Email)
	if len(beginData.Response.AllowedConfirmations) > 0 {
		pollCtx, cancel := context.WithCancel(context.Background())
		c.mu.Lock()
		c.state = StateAwaiting2FA
		c.cancelPoll = cancel
		c.mu.Unlock()

		interval := time.Duration(beginData.Response.Interval * float64(time.Second))
		if interval < 1000*time.Millisecond {
			interval = 1500 * time.Millisecond
		}

		go c.pollForApproval(pollCtx, beginData.Response.ClientID, beginData.Response.RequestID, username, interval)

		prompt := "Please approve the sign-in request on your Steam Mobile app or enter your Steam Guard code."
		if !hasMobile && hasEmail {
			prompt = "Please enter the Steam Guard code sent to your email address."
		}

		log.Printf("[Steam Auth] Steam Guard 2FA challenge requested for %s (Client ID: %s, MobilePush: %v, DeviceCode: %v, Email: %v)",
			username, c.clientID, hasMobile, hasDevice, hasEmail)

		ws.GetHub().Broadcast("steam:2fa_required", map[string]interface{}{
			"username":                username,
			"prompt":                  prompt,
			"client_id":               c.clientID,
			"has_mobile_confirmation": hasMobile,
			"has_device_code":         hasDevice,
			"has_email_code":          hasEmail,
			"associated_message":      associatedMsg,
			"created_at":              time.Now().Unix(),
		})
		c.broadcastStatus()
		return
	}

	// If no confirmation needed, poll and finalize immediately
	if err := c.pollOnceAndFinalize(); err != nil {
		c.failLogin(err.Error())
	}
}

// pollForApproval continuously checks Steam servers in the background for mobile push approval (Approve/Deny)
func (c *Client) pollForApproval(ctx context.Context, clientID, requestID, username string, interval time.Duration) {
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Steam sessions expire after ~3 to 5 minutes
	timeout := time.After(3 * time.Minute)

	log.Printf("[Steam Auth] Starting background poll for Mobile / Steam Guard confirmation for %s (interval: %v)...", username, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Steam Auth] Polling stopped for %s (context cancelled)", username)
			return
		case <-timeout:
			log.Printf("[Steam Auth] Polling timed out for %s", username)
			c.failLogin("Steam Guard authorization request timed out")
			return
		case <-ticker.C:
			form := url.Values{}
			form.Set("client_id", clientID)
			form.Set("request_id", requestID)

			req, err := http.NewRequestWithContext(ctx, "POST", "https://api.steampowered.com/IAuthenticationService/PollAuthSessionStatus/v1/", strings.NewReader(form.Encode()))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

			resp, err := c.httpClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			eresult := resp.Header.Get("X-eresult")
			if eresult == "15" {
				resp.Body.Close()
				log.Printf("[Steam Auth] Login request was denied on mobile device for %s", username)
				c.failLogin("Login request was denied on your Steam Mobile app")
				return
			} else if eresult == "27" {
				resp.Body.Close()
				log.Printf("[Steam Auth] Login request expired for %s", username)
				c.failLogin("Steam Guard authorization request expired")
				return
			} else if eresult != "" && eresult != "1" {
				resp.Body.Close()
				log.Printf("[Steam Auth] Polling failed with EResult %s for %s", eresult, username)
				c.failLogin(fmt.Sprintf("Steam authentication rejected (EResult %s)", eresult))
				return
			}

			var pollData struct {
				Response struct {
					RefreshToken string `json:"refresh_token"`
					AccessToken  string `json:"access_token"`
					AccountName  string `json:"account_name"`
					NewClientID  string `json:"new_client_id"`
				} `json:"response"`
			}
			err = json.NewDecoder(resp.Body).Decode(&pollData)
			resp.Body.Close()

			if err == nil && pollData.Response.RefreshToken != "" {
				log.Printf("[Steam Auth] Received approved Steam tokens via push notification for %s!", username)
				acct := pollData.Response.AccountName
				if acct == "" {
					acct = username
				}
				if err := c.finalizeSession(pollData.Response.RefreshToken, pollData.Response.AccessToken, acct); err != nil {
					c.failLogin(fmt.Sprintf("Failed to finalize session after approval: %v", err))
				}
				return
			}
		}
	}
}

// Submit2FA handles manual Steam Guard code submission (from Mobile Authenticator TOTP or Email)
func (c *Client) Submit2FA(code string) error {
	c.mu.RLock()
	if c.state != StateAwaiting2FA {
		c.mu.RUnlock()
		return fmt.Errorf("no pending 2FA authentication challenge")
	}
	clientID := c.clientID
	steamID := c.steamID
	confType := c.confirmationType
	username := c.username
	c.mu.RUnlock()

	code = strings.TrimSpace(code)
	if len(code) == 0 {
		return fmt.Errorf("Steam Guard code cannot be empty")
	}

	// Always ensure code_type is 3 (DeviceCode) or 2 (EmailCode). Never 1 (AppConfirmation).
	if confType != 2 {
		confType = 3
	}

	log.Printf("[Steam Auth] Submitting Steam Guard code for %s (Client ID: %s, CodeType: %d)...", username, clientID, confType)

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("steamid", steamID)
	form.Set("code", code)
	form.Set("code_type", strconv.Itoa(confType))

	req, err := http.NewRequest("POST", "https://api.steampowered.com/IAuthenticationService/UpdateAuthSessionWithSteamGuardCode/v1/", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to build 2FA request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error submitting 2FA code: %w", err)
	}
	defer resp.Body.Close()

	eresult := resp.Header.Get("X-eresult")
	if eresult != "" && eresult != "1" {
		log.Printf("[Steam Auth] UpdateAuthSessionWithSteamGuardCode failed with EResult %s", eresult)
		return fmt.Errorf("invalid or expired Steam Guard code")
	}

	// Poll and finalize web session
	if err := c.pollOnceAndFinalize(); err != nil {
		return err
	}

	return nil
}

// pollOnceAndFinalize polls PollAuthSessionStatus synchronously up to 6 times to fetch authenticated tokens
func (c *Client) pollOnceAndFinalize() error {
	c.mu.RLock()
	clientID := c.clientID
	requestID := c.requestID
	username := c.username
	c.mu.RUnlock()

	var refreshToken, accessToken, accountName string
	var lastErr error

	// Poll up to 6 times (1s delay)
	for i := 0; i < 6; i++ {
		form := url.Values{}
		form.Set("client_id", clientID)
		form.Set("request_id", requestID)

		req, err := http.NewRequest("POST", "https://api.steampowered.com/IAuthenticationService/PollAuthSessionStatus/v1/", strings.NewReader(form.Encode()))
		if err != nil {
			return fmt.Errorf("failed to build poll request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		eresult := resp.Header.Get("X-eresult")
		if eresult != "" && eresult != "1" {
			resp.Body.Close()
			return fmt.Errorf("Steam authentication session failed (EResult %s)", eresult)
		}

		var pollData struct {
			Response struct {
				RefreshToken string `json:"refresh_token"`
				AccessToken  string `json:"access_token"`
				AccountName  string `json:"account_name"`
			} `json:"response"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&pollData)
		resp.Body.Close()

		if decodeErr == nil && pollData.Response.RefreshToken != "" {
			refreshToken = pollData.Response.RefreshToken
			accessToken = pollData.Response.AccessToken
			accountName = pollData.Response.AccountName
			break
		}
		time.Sleep(1000 * time.Millisecond)
	}

	if refreshToken == "" {
		if lastErr != nil {
			return fmt.Errorf("polling auth session timed out: %w", lastErr)
		}
		return fmt.Errorf("polling auth session timed out or was rejected by Steam")
	}

	if accountName == "" {
		accountName = username
	}
	return c.finalizeSession(refreshToken, accessToken, accountName)
}

// finalizeSession exchanges the approved refresh token for authentic web session cookies and persists the session
func (c *Client) finalizeSession(refreshToken, accessToken, accountName string) error {
	c.mu.Lock()
	if c.state == StateConnected {
		c.mu.Unlock()
		return nil
	}
	if c.cancelPoll != nil {
		c.cancelPoll()
		c.cancelPoll = nil
	}
	clientID := c.clientID
	steamID := c.steamID
	if accountName == "" {
		accountName = c.username
	}
	c.mu.Unlock()

	log.Printf("[Steam Auth] Steam session confirmed for %s. Finalizing web session...", accountName)

	// Exchange refresh token for web cookies via finalizelogin
	sessionBytes := make([]byte, 12)
	_, _ = rand.Read(sessionBytes)
	sessionID := hex.EncodeToString(sessionBytes)

	finForm := url.Values{}
	finForm.Set("nonce", refreshToken)
	finForm.Set("sessionid", sessionID)
	finForm.Set("redir", "https://steamcommunity.com/login/home/?goto=")

	finReq, err := http.NewRequest("POST", "https://login.steampowered.com/jwt/finalizelogin", strings.NewReader(finForm.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create finalizelogin request: %w", err)
	}
	finReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	finReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	finResp, err := c.httpClient.Do(finReq)
	if err != nil {
		return fmt.Errorf("error in finalizelogin: %w", err)
	}
	defer finResp.Body.Close()

	var finData struct {
		SteamID      string `json:"steamID"`
		TransferInfo []struct {
			URL    string            `json:"url"`
			Params map[string]string `json:"params"`
		} `json:"transfer_info"`
	}
	if err := json.NewDecoder(finResp.Body).Decode(&finData); err != nil {
		return fmt.Errorf("failed to parse finalizelogin response: %w", err)
	}

	if finData.SteamID != "" {
		steamID = finData.SteamID
	}

	var sessionCookies string
	// Execute transfer token requests for Steam Community
	for _, transfer := range finData.TransferInfo {
		setForm := url.Values{}
		for k, v := range transfer.Params {
			setForm.Set(k, v)
		}
		setForm.Set("steamID", steamID)

		setReq, err := http.NewRequest("POST", transfer.URL, strings.NewReader(setForm.Encode()))
		if err != nil {
			continue
		}
		setReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		setReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

		setResp, err := c.httpClient.Do(setReq)
		if err != nil {
			continue
		}

		for _, cookie := range setResp.Cookies() {
			if cookie.Name == "steamLoginSecure" {
				sessionCookies = fmt.Sprintf("steamLoginSecure=%s; sessionid=%s", cookie.Value, sessionID)
				log.Printf("[Steam Auth] Acquired authentic steamLoginSecure session cookie")
			}
		}
		setResp.Body.Close()
	}

	if sessionCookies == "" {
		// Fallback token extraction from transfer params
		for _, transfer := range finData.TransferInfo {
			if strings.Contains(transfer.URL, "steamcommunity.com") {
				if auth, ok := transfer.Params["auth"]; ok && auth != "" {
					sessionCookies = fmt.Sprintf("steamLoginSecure=%s%%7C%s; sessionid=%s", steamID, auth, sessionID)
				}
			}
		}
	}

	c.mu.Lock()
	c.state = StateConnected
	c.username = accountName
	c.steamID = steamID
	c.accessToken = accessToken
	c.refreshToken = refreshToken
	c.sessionCookies = sessionCookies
	c.password = ""
	c.errorMessage = ""
	c.hasMobileConfirmation = false
	c.hasDeviceCode = false
	c.hasEmailCode = false
	c.associatedMsg = ""
	c.mu.Unlock()

	// Persist authenticated credentials in SQLite
	if c.db != nil {
		_ = c.db.SaveSteamSession(&db.SteamSession{
			Username:       c.username,
			SteamID:        c.steamID,
			AccessToken:    c.accessToken,
			RefreshToken:   c.refreshToken,
			SessionCookies: c.sessionCookies,
			ClientID:       clientID,
			IsActive:       true,
			UpdatedAt:      time.Now(),
		})
	}

	log.Printf("[Steam Auth] Authentication successful! Established session for %s (SteamID: %s)", c.username, c.steamID)
	c.broadcastStatus()
	return nil
}

func (c *Client) failLogin(msg string) {
	log.Printf("[Steam Auth] Login error: %s", msg)
	c.mu.Lock()
	if c.cancelPoll != nil {
		c.cancelPoll()
		c.cancelPoll = nil
	}
	c.state = StateError
	c.errorMessage = msg
	c.hasMobileConfirmation = false
	c.hasDeviceCode = false
	c.hasEmailCode = false
	c.associatedMsg = ""
	c.mu.Unlock()
	c.broadcastStatus()
}

// Logout invalidates the active Steam session
func (c *Client) Logout() error {
	c.mu.Lock()
	if c.cancelPoll != nil {
		c.cancelPoll()
		c.cancelPoll = nil
	}
	c.state = StateDisconnected
	c.username = ""
	c.steamID = ""
	c.accessToken = ""
	c.refreshToken = ""
	c.sessionCookies = ""
	c.errorMessage = ""
	c.hasMobileConfirmation = false
	c.hasDeviceCode = false
	c.hasEmailCode = false
	c.associatedMsg = ""
	c.mu.Unlock()

	if c.db != nil {
		_ = c.db.ClearSteamSession()
	}

	c.broadcastStatus()
	return nil
}

func (c *Client) broadcastStatus() {
	status := c.Status()
	ws.GetHub().Broadcast("steam:status", status)
}

func encryptPasswordRSA(password, pubKeyModHex, pubKeyExpHex string) (string, error) {
	mod := new(big.Int)
	if _, ok := mod.SetString(pubKeyModHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA modulus hex")
	}

	expInt, err := strconv.ParseInt(pubKeyExpHex, 16, 64)
	if err != nil {
		return "", fmt.Errorf("invalid RSA exponent hex: %w", err)
	}

	pubKey := &rsa.PublicKey{
		N: mod,
		E: int(expInt),
	}

	encryptedBytes, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, []byte(password))
	if err != nil {
		return "", fmt.Errorf("RSA encryption failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encryptedBytes), nil
}
