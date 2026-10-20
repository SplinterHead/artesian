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
	"time"

	"artesian/internal/db"
	"artesian/internal/ports"
	"artesian/internal/ws"
)

func (a *Adapter) restoreSavedSession() {
	if a.db == nil {
		return
	}
	sess, err := a.db.GetAuthSession(a.ID())
	if err != nil {
		log.Printf("[Steam Auth] No active Steam session found in database (%v)", err)
		return
	}
	if sess != nil && sess.IsActive && sess.AccessToken != "" {
		a.authMu.Lock()
		a.username = sess.Username
		a.steamID = sess.AccountID
		a.accessToken = sess.AccessToken
		a.refreshToken = sess.RefreshToken
		a.sessionCookies = sess.SessionData
		a.clientID = sess.ClientID
		a.state = ports.StateConnected
		a.authMu.Unlock()
		log.Printf("[Steam Auth] Restored genuine Steam session for %s (SteamID: %s)", sess.Username, sess.AccountID)
	}
}

// Status returns current Steam connectivity info
func (a *Adapter) Status() ports.AuthStatus {
	a.authMu.RLock()
	defer a.authMu.RUnlock()

	return ports.AuthStatus{
		State:                 a.state,
		Username:              a.username,
		AccountID:             a.steamID,
		HasValidAuth:          a.state == ports.StateConnected,
		ErrorMessage:          a.errorMessage,
		HasMobileConfirmation: a.hasMobileConfirmation,
		HasDeviceCode:         a.hasDeviceCode,
		HasEmailCode:          a.hasEmailCode,
		AssociatedMessage:     a.associatedMsg,
	}
}

// GetAccessToken returns authenticated OAuth access token
func (a *Adapter) GetAccessToken() string {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.accessToken
}

// GetAccountID returns the generic platform account identifier
func (a *Adapter) GetAccountID() string {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.steamID
}

// Login begins genuine modern Steam authentication via IAuthenticationService
func (a *Adapter) Login(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("username and password are required")
	}

	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.username = username
	a.password = password
	a.state = ports.StateConnecting
	a.errorMessage = ""
	a.hasMobileConfirmation = false
	a.hasDeviceCode = false
	a.hasEmailCode = false
	a.associatedMsg = ""
	a.authMu.Unlock()

	a.broadcastStatus()
	log.Printf("[Steam Auth] Initiating genuine Steam authentication for account %s...", username)

	go a.performLogin(username, password)
	return nil
}

func (a *Adapter) performLogin(username, password string) {
	// 1. Fetch RSA public key for password encryption
	rsaURL := fmt.Sprintf("https://api.steampowered.com/IAuthenticationService/GetPasswordRSAPublicKey/v1/?account_name=%s", url.QueryEscape(username))
	req, err := http.NewRequest("GET", rsaURL, nil)
	if err != nil {
		a.failLogin(fmt.Sprintf("Failed to build RSA request: %v", err))
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.failLogin(fmt.Sprintf("Network error connecting to Steam RSA endpoint: %v", err))
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
		a.failLogin(fmt.Sprintf("Failed to decode RSA response: %v", err))
		return
	}

	if rsaData.Response.PublicKeyMod == "" || rsaData.Response.PublicKeyExp == "" {
		a.failLogin("Steam returned empty RSA public key parameters")
		return
	}

	// 2. Encrypt password with RSA PKCS#1 v1.5
	encPass, err := encryptPasswordRSA(password, rsaData.Response.PublicKeyMod, rsaData.Response.PublicKeyExp)
	if err != nil {
		a.failLogin(fmt.Sprintf("Password encryption error: %v", err))
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
		a.failLogin(fmt.Sprintf("Failed to build BeginAuth request: %v", err))
		return
	}
	beginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	beginReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")

	beginResp, err := a.httpClient.Do(beginReq)
	if err != nil {
		a.failLogin(fmt.Sprintf("Error starting Steam auth session: %v", err))
		return
	}
	defer beginResp.Body.Close()

	eresult := beginResp.Header.Get("X-eresult")
	if eresult == "5" {
		a.failLogin("Invalid Steam account name or password")
		return
	} else if eresult == "84" {
		a.failLogin("Steam rate limit exceeded; please wait a moment and try again")
		return
	} else if eresult != "" && eresult != "1" {
		a.failLogin(fmt.Sprintf("Steam authentication failed (EResult %s)", eresult))
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
		a.failLogin(fmt.Sprintf("Failed to parse BeginAuth response: %v", err))
		return
	}

	if beginData.Response.ClientID == "" {
		errMsg := beginData.Response.ExtendedErrorMessage
		if errMsg == "" {
			errMsg = "Steam authentication session could not be established"
		}
		a.failLogin(errMsg)
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
		case 1: // Push notification Approve/Deny
			hasMobile = true
		case 2: // EmailCode
			hasEmail = true
		case 3: // DeviceCode
			hasDevice = true
		}
	}

	confType := 3
	if hasEmail && !hasDevice && !hasMobile {
		confType = 2
	}

	a.authMu.Lock()
	a.clientID = beginData.Response.ClientID
	a.requestID = beginData.Response.RequestID
	a.steamID = beginData.Response.SteamID
	a.confirmationType = confType
	a.hasMobileConfirmation = hasMobile
	a.hasDeviceCode = hasDevice
	a.hasEmailCode = hasEmail
	a.associatedMsg = associatedMsg
	a.authMu.Unlock()

	if len(beginData.Response.AllowedConfirmations) > 0 {
		pollCtx, cancel := context.WithCancel(context.Background())
		a.authMu.Lock()
		a.state = ports.StateAwaiting2FA
		a.cancelPoll = cancel
		a.authMu.Unlock()

		interval := time.Duration(beginData.Response.Interval * float64(time.Second))
		if interval < 1000*time.Millisecond {
			interval = 1500 * time.Millisecond
		}

		go a.pollForApproval(pollCtx, beginData.Response.ClientID, beginData.Response.RequestID, username, interval)

		prompt := "Please approve the sign-in request on your Steam Mobile app or enter your Steam Guard code."
		if !hasMobile && hasEmail {
			prompt = "Please enter the Steam Guard code sent to your email address."
		}

		log.Printf("[Steam Auth] Steam Guard 2FA challenge requested for %s (Client ID: %s, MobilePush: %v, DeviceCode: %v, Email: %v)",
			username, a.clientID, hasMobile, hasDevice, hasEmail)

		ws.GetHub().Broadcast("steam:2fa_required", map[string]interface{}{
			"username":                username,
			"prompt":                  prompt,
			"client_id":               a.clientID,
			"has_mobile_confirmation": hasMobile,
			"has_device_code":         hasDevice,
			"has_email_code":          hasEmail,
			"associated_message":      associatedMsg,
			"created_at":              time.Now().Unix(),
		})
		a.broadcastStatus()
		return
	}

	if err := a.pollOnceAndFinalize(); err != nil {
		a.failLogin(err.Error())
	}
}

func (a *Adapter) pollForApproval(ctx context.Context, clientID, requestID, username string, interval time.Duration) {
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	timeout := time.After(3 * time.Minute)

	log.Printf("[Steam Auth] Starting background poll for Mobile / Steam Guard confirmation for %s (interval: %v)...", username, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Steam Auth] Polling stopped for %s (context cancelled)", username)
			return
		case <-timeout:
			log.Printf("[Steam Auth] Polling timed out for %s", username)
			a.failLogin("Steam Guard authorization request timed out")
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

			resp, err := a.httpClient.Do(req)
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
				a.failLogin("Login request was denied on your Steam Mobile app")
				return
			} else if eresult == "27" {
				resp.Body.Close()
				log.Printf("[Steam Auth] Login request expired for %s", username)
				a.failLogin("Steam Guard authorization request expired")
				return
			} else if eresult != "" && eresult != "1" {
				resp.Body.Close()
				log.Printf("[Steam Auth] Polling failed with EResult %s for %s", eresult, username)
				a.failLogin(fmt.Sprintf("Steam authentication rejected (EResult %s)", eresult))
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
				if err := a.finalizeSession(pollData.Response.RefreshToken, pollData.Response.AccessToken, acct); err != nil {
					a.failLogin(fmt.Sprintf("Failed to finalize session after approval: %v", err))
				}
				return
			}
		}
	}
}

// Submit2FA handles manual Steam Guard code submission (from Mobile Authenticator TOTP or Email)
func (a *Adapter) Submit2FA(code string) error {
	a.authMu.RLock()
	if a.state != ports.StateAwaiting2FA {
		a.authMu.RUnlock()
		return fmt.Errorf("no pending 2FA authentication challenge")
	}
	clientID := a.clientID
	steamID := a.steamID
	confType := a.confirmationType
	username := a.username
	a.authMu.RUnlock()

	code = strings.TrimSpace(code)
	if len(code) == 0 {
		return fmt.Errorf("Steam Guard code cannot be empty")
	}

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

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error submitting 2FA code: %w", err)
	}
	defer resp.Body.Close()

	eresult := resp.Header.Get("X-eresult")
	if eresult != "" && eresult != "1" {
		log.Printf("[Steam Auth] UpdateAuthSessionWithSteamGuardCode failed with EResult %s", eresult)
		return fmt.Errorf("invalid or expired Steam Guard code")
	}

	if err := a.pollOnceAndFinalize(); err != nil {
		return err
	}

	return nil
}

// Cancel2FA aborts the active 2FA login challenge
func (a *Adapter) Cancel2FA() error {
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.state = ports.StateDisconnected
	a.errorMessage = ""
	a.hasMobileConfirmation = false
	a.hasDeviceCode = false
	a.hasEmailCode = false
	a.associatedMsg = ""
	a.authMu.Unlock()

	a.broadcastStatus()
	return nil
}

func (a *Adapter) pollOnceAndFinalize() error {
	a.authMu.RLock()
	clientID := a.clientID
	requestID := a.requestID
	username := a.username
	a.authMu.RUnlock()

	var refreshToken, accessToken, accountName string
	var lastErr error

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

		resp, err := a.httpClient.Do(req)
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
	return a.finalizeSession(refreshToken, accessToken, accountName)
}

func (a *Adapter) finalizeSession(refreshToken, accessToken, accountName string) error {
	a.authMu.Lock()
	if a.state == ports.StateConnected {
		a.authMu.Unlock()
		return nil
	}
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	clientID := a.clientID
	steamID := a.steamID
	if accountName == "" {
		accountName = a.username
	}
	a.authMu.Unlock()

	log.Printf("[Steam Auth] Steam session confirmed for %s. Finalizing web session...", accountName)

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

	finResp, err := a.httpClient.Do(finReq)
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

		setResp, err := a.httpClient.Do(setReq)
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
		for _, transfer := range finData.TransferInfo {
			if strings.Contains(transfer.URL, "steamcommunity.com") {
				if auth, ok := transfer.Params["auth"]; ok && auth != "" {
					sessionCookies = fmt.Sprintf("steamLoginSecure=%s%%7C%s; sessionid=%s", steamID, auth, sessionID)
				}
			}
		}
	}

	a.authMu.Lock()
	a.state = ports.StateConnected
	a.username = accountName
	a.steamID = steamID
	a.accessToken = accessToken
	a.refreshToken = refreshToken
	a.sessionCookies = sessionCookies
	a.password = ""
	a.errorMessage = ""
	a.hasMobileConfirmation = false
	a.hasDeviceCode = false
	a.hasEmailCode = false
	a.associatedMsg = ""
	a.authMu.Unlock()

	if a.db != nil {
		_ = a.db.SaveAuthSession(&db.AuthSession{
			Service:      a.ID(),
			Username:     a.username,
			AccountID:    a.steamID,
			AccessToken:  a.accessToken,
			RefreshToken: a.refreshToken,
			SessionData:  a.sessionCookies,
			ClientID:     clientID,
			IsActive:     true,
			UpdatedAt:    time.Now(),
		})
		_ = a.db.SetServiceAutoUpdate(a.ID(), true)
	}

	log.Printf("[Steam Auth] Authentication successful! Established session for %s (SteamID: %s)", a.username, a.steamID)
	a.broadcastStatus()
	return nil
}

func (a *Adapter) failLogin(msg string) {
	log.Printf("[Steam Auth] Login error: %s", msg)
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.state = ports.StateError
	a.errorMessage = msg
	a.hasMobileConfirmation = false
	a.hasDeviceCode = false
	a.hasEmailCode = false
	a.associatedMsg = ""
	a.authMu.Unlock()
	a.broadcastStatus()
}

// Logout invalidates the active Steam session
func (a *Adapter) Logout() error {
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	username := a.username
	a.state = ports.StateDisconnected
	a.username = ""
	a.steamID = ""
	a.accessToken = ""
	a.refreshToken = ""
	a.sessionCookies = ""
	a.errorMessage = ""
	a.hasMobileConfirmation = false
	a.hasDeviceCode = false
	a.hasEmailCode = false
	a.associatedMsg = ""
	a.authMu.Unlock()

	if a.db != nil {
		_ = a.db.ClearAuthSession(a.ID())
	}

	log.Printf("[Steam Auth] Session cleared for %s", username)
	a.broadcastStatus()
	return nil
}

func (a *Adapter) broadcastStatus() {
	status := a.Status()
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
