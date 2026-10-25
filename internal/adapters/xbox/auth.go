package xbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"artesian/internal/db"
	"artesian/internal/ports"
	"artesian/internal/ws"
)

const (
	XboxClientID       = "00000000402b5328"
	XboxLiveScope      = "service::user.auth.xboxlive.com::MBI_SSL"
	OAuthDeviceCodeURL = "https://login.live.com/oauth20_connect.srf"
	OAuthTokenURL      = "https://login.live.com/oauth20_token.srf"
	XAUAuthenticateURL = "https://user.auth.xboxlive.com/user/authenticate"
	XSTSAuthorizeURL   = "https://xsts.auth.xboxlive.com/xsts/authorize"
)

type savedSessionData struct {
	UserHash        string    `json:"uhs"`
	XAUToken        string    `json:"xau_token"`
	MpXstsToken     string    `json:"mp_xsts"`
	XblXstsToken    string    `json:"xbl_xsts"`
	UpdateXstsToken string    `json:"update_xsts"`
	TokenExpiry     time.Time `json:"token_expiry"`
}

// Status returns public Xbox session info for UI
func (a *Adapter) Status() ports.AuthStatus {
	a.authMu.RLock()
	defer a.authMu.RUnlock()

	return ports.AuthStatus{
		State:             a.state,
		Username:          a.gamertag,
		AccountID:         a.xuid,
		HasValidAuth:      a.state == ports.StateConnected && a.userHash != "" && a.xblXstsToken != "",
		ErrorMessage:      a.errorMessage,
		HasDeviceCode:     a.userCode != "",
		UserCode:          a.userCode,
		VerificationURL:   a.verificationURL,
		ExpiresIn:         a.expiresIn,
		AssociatedMessage: a.errorMessage,
	}
}

// Login initiates the Xbox OAuth2 Device Code flow
func (a *Adapter) Login(username, password string) error {
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.errorMessage = ""
	a.state = ports.StateConnecting
	a.userCode = ""
	a.verificationURL = ""
	a.authMu.Unlock()

	ws.GetHub().Broadcast("xbox:status", a.Status())

	// If a direct refresh token was passed as password/token
	if strings.HasPrefix(password, "M.R3") || strings.HasPrefix(password, "M.C") {
		return a.loginWithRefreshToken(password)
	}

	// 1. Request OAuth2 Device Code
	data := url.Values{}
	data.Set("client_id", XboxClientID)
	data.Set("scope", XboxLiveScope)
	data.Set("response_type", "device_code")

	req, err := http.NewRequest("POST", OAuthDeviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		a.setAuthError(fmt.Sprintf("Failed to prepare device code request: %v", err))
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.setAuthError(fmt.Sprintf("Device code network error: %v", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		a.setAuthError(fmt.Sprintf("Device code request failed (%d): %s", resp.StatusCode, string(body)))
		return fmt.Errorf("device code error: %s", string(body))
	}

	var devResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Message         string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&devResp); err != nil {
		a.setAuthError(fmt.Sprintf("Failed to parse device code response: %v", err))
		return err
	}

	pollInterval := devResp.Interval
	if pollInterval <= 0 {
		pollInterval = 5
	}
	expiresIn := devResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 900
	}

	ctx, cancel := context.WithCancel(context.Background())

	a.authMu.Lock()
	a.userCode = devResp.UserCode
	a.verificationURL = devResp.VerificationURI
	if a.verificationURL == "" {
		a.verificationURL = "https://www.microsoft.com/link"
	}
	a.expiresIn = expiresIn
	a.cancelPoll = cancel
	a.authMu.Unlock()

	log.Printf("[Xbox Auth] Device code issued: %s (Enter at %s)", devResp.UserCode, a.verificationURL)
	ws.GetHub().Broadcast("xbox:status", a.Status())

	go a.pollForDeviceCode(ctx, devResp.DeviceCode, time.Duration(pollInterval)*time.Second, time.Now().Add(time.Duration(expiresIn)*time.Second))

	return nil
}

func (a *Adapter) pollForDeviceCode(ctx context.Context, deviceCode string, interval time.Duration, deadline time.Time) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				a.setAuthError("Device login code expired. Please try connecting again.")
				return
			}

			data := url.Values{}
			data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
			data.Set("client_id", XboxClientID)
			data.Set("device_code", deviceCode)

			req, err := http.NewRequestWithContext(ctx, "POST", OAuthTokenURL, strings.NewReader(data.Encode()))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := a.httpClient.Do(req)
			if err != nil {
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				var tokenResp struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
					ExpiresIn    int    `json:"expires_in"`
				}
				if err := json.Unmarshal(body, &tokenResp); err == nil && tokenResp.AccessToken != "" {
					log.Printf("[Xbox Auth] MSA token acquired successfully! Proceeding to XAU and XSTS token exchange...")
					if err := a.exchangeXAUAndXSTS(tokenResp.AccessToken, tokenResp.RefreshToken); err != nil {
						a.setAuthError(fmt.Sprintf("Xbox Live authorization failed: %v", err))
					}
					return
				}
			}

			var errResp struct {
				Error            string `json:"error"`
				ErrorDescription string `json:"error_description"`
			}
			_ = json.Unmarshal(body, &errResp)

			switch errResp.Error {
			case "authorization_pending":
				// Still awaiting user approval at microsoft.com/devicelogin
				continue
			case "authorization_declined":
				a.setAuthError("Authorization declined by user.")
				return
			case "bad_verification_code", "expired_token":
				a.setAuthError("Device login code expired. Please restart login.")
				return
			default:
				if errResp.Error != "" {
					log.Printf("[Xbox Auth] Notice during device code poll: %s (%s)", errResp.Error, errResp.ErrorDescription)
				}
			}
		}
	}
}

func (a *Adapter) loginWithRefreshToken(refreshToken string) error {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", XboxClientID)
	data.Set("refresh_token", refreshToken)
	data.Set("scope", XboxLiveScope)

	req, err := http.NewRequest("POST", OAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("refresh token exchange failed (%d): %s", resp.StatusCode, string(b))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	return a.exchangeXAUAndXSTS(tokenResp.AccessToken, tokenResp.RefreshToken)
}

// exchangeXAUAndXSTS walks the 3-step token chain: MSA -> XAU -> XSTS (mp & xbl)
func (a *Adapter) exchangeXAUAndXSTS(msaAccessToken, msaRefreshToken string) error {
	// Step 2: Request Xbox Live User Token (XAU)
	userHash, xauToken, err := a.requestXAU(msaAccessToken)
	if err != nil {
		return err
	}

	// Step 3a: Request XSTS Token scoped to http://mp.microsoft.com/ (Store & Collections)
	mpXsts, err := a.requestXSTSToken(xauToken, "http://mp.microsoft.com/")
	if err != nil {
		return fmt.Errorf("XSTS mp.microsoft.com authorization failed: %w", err)
	}

	// Step 3b: Request XSTS Token scoped to http://xboxlive.com (TitleHub, Profile, Community)
	xblXsts, err := a.requestXSTSToken(xauToken, "http://xboxlive.com")
	if err != nil {
		return fmt.Errorf("XSTS xboxlive.com authorization failed: %w", err)
	}

	// Step 3c: Request XSTS Token scoped to http://update.xboxlive.com (Package Service)
	updateXsts, err := a.requestXSTSToken(xauToken, "http://update.xboxlive.com")
	var updateToken string
	if err != nil {
		log.Printf("[Xbox Auth] Notice: update.xboxlive.com authorization warning: %v", err)
	} else if updateXsts != nil {
		updateToken = updateXsts.token
	}

	gamertag := mpXsts.gamertag
	if gamertag == "" {
		gamertag = xblXsts.gamertag
	}
	xuid := mpXsts.xuid
	if xuid == "" {
		xuid = xblXsts.xuid
	}

	expiry := time.Now().Add(18 * time.Hour) // XSTS tokens typically valid for 24h

	a.authMu.Lock()
	a.state = ports.StateConnected
	a.gamertag = gamertag
	a.xuid = xuid
	a.userHash = userHash
	a.msaToken = msaAccessToken
	a.msaRefresh = msaRefreshToken
	a.xauToken = xauToken
	a.mpXstsToken = mpXsts.token
	a.xblXstsToken = xblXsts.token
	a.updateXstsToken = updateToken
	a.tokenExpiry = expiry
	a.userCode = ""
	a.verificationURL = ""
	a.errorMessage = ""
	a.authMu.Unlock()

	// Persist session into SQLite
	if a.db != nil {
		sessionJSON, _ := json.Marshal(savedSessionData{
			UserHash:        userHash,
			XAUToken:        xauToken,
			MpXstsToken:     mpXsts.token,
			XblXstsToken:    xblXsts.token,
			UpdateXstsToken: updateToken,
			TokenExpiry:     expiry,
		})
		_ = a.db.SaveAuthSession(&db.AuthSession{
			Service:      "xbox",
			Username:     gamertag,
			AccountID:    xuid,
			AccessToken:  msaAccessToken,
			RefreshToken: msaRefreshToken,
			SessionData:  string(sessionJSON),
			ClientID:     XboxClientID,
			IsActive:     true,
		})
		_ = a.db.SetServiceAutoUpdate("xbox", true)
	}

	log.Printf("[Xbox Auth] Successfully authenticated Gamertag: %s (XUID: %s, UHS: %s)", gamertag, xuid, userHash)
	ws.GetHub().Broadcast("xbox:status", a.Status())
	ws.GetHub().Broadcast("services:updated", nil)

	return nil
}

type xstsResult struct {
	token    string
	gamertag string
	xuid     string
	userHash string
}

func (a *Adapter) requestXSTSToken(xauToken, relyingParty string) (*xstsResult, error) {
	xstsPayload := map[string]interface{}{
		"Properties": map[string]interface{}{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xauToken},
		},
		"RelyingParty": relyingParty,
		"TokenType":    "JWT",
	}

	payloadJSON, _ := json.Marshal(xstsPayload)
	req, err := http.NewRequest("POST", XSTSAuthorizeURL, bytes.NewReader(payloadJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var xstsErr struct {
			Identity string `json:"Identity"`
			XErr     int64  `json:"XErr"`
			Message  string `json:"Message"`
		}
		_ = json.Unmarshal(bodyBytes, &xstsErr)

		switch xstsErr.XErr {
		case 2148916233:
			return nil, fmt.Errorf("this Microsoft account does not have an Xbox Live profile. Please sign in at xbox.com first to create a Gamertag")
		case 2148916235:
			return nil, fmt.Errorf("Xbox Live is not currently available in your account country/region")
		case 2148916238:
			return nil, fmt.Errorf("child accounts require an adult to add them to a Microsoft Family to authorize access")
		default:
			return nil, fmt.Errorf("XSTS error (code %d): %s", xstsErr.XErr, string(bodyBytes))
		}
	}

	var xstsResp struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			Xui []struct {
				Uhs string `json:"uhs"`
				Gtg string `json:"gtg"`
				Xid string `json:"xid"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := json.Unmarshal(bodyBytes, &xstsResp); err != nil {
		return nil, fmt.Errorf("failed to decode XSTS response: %w", err)
	}

	if xstsResp.Token == "" {
		return nil, fmt.Errorf("empty XSTS token returned for %s", relyingParty)
	}

	res := &xstsResult{
		token: xstsResp.Token,
	}
	if len(xstsResp.DisplayClaims.Xui) > 0 {
		res.userHash = xstsResp.DisplayClaims.Xui[0].Uhs
		res.gamertag = xstsResp.DisplayClaims.Xui[0].Gtg
		res.xuid = xstsResp.DisplayClaims.Xui[0].Xid
	}
	return res, nil
}

// ensureValidTokens checks token freshness and silently refreshes if near expiration
func (a *Adapter) ensureValidTokens() error {
	a.authMu.RLock()
	isExpiring := time.Now().Add(10 * time.Minute).After(a.tokenExpiry)
	hasRefresh := a.msaRefresh != ""
	hasUpdate := a.updateXstsToken != ""
	xau := a.xauToken
	a.authMu.RUnlock()

	if isExpiring && hasRefresh {
		a.authMu.Lock()
		refresh := a.msaRefresh
		a.authMu.Unlock()
		return a.loginWithRefreshToken(refresh)
	}

	// If updateXstsToken is missing (e.g. restored from earlier DB session), request on demand
	if !hasUpdate && xau != "" {
		updateXsts, err := a.requestXSTSToken(xau, "http://update.xboxlive.com")
		if err == nil && updateXsts != nil && updateXsts.token != "" {
			a.authMu.Lock()
			a.updateXstsToken = updateXsts.token
			a.authMu.Unlock()
		}
	}

	return nil
}

// Submit2FA handles code verification if manual code entered
func (a *Adapter) Submit2FA(code string) error {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return fmt.Errorf("empty code provided")
	}
	return a.Login("", trimmed)
}

// Cancel2FA aborts pending device code polling
func (a *Adapter) Cancel2FA() error {
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.userCode = ""
	a.verificationURL = ""
	a.state = ports.StateDisconnected
	a.errorMessage = ""
	a.authMu.Unlock()

	ws.GetHub().Broadcast("xbox:status", a.Status())
	return nil
}

// Logout clears credentials and active Xbox session
func (a *Adapter) Logout() error {
	a.authMu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.state = ports.StateDisconnected
	a.gamertag = ""
	a.xuid = ""
	a.userHash = ""
	a.msaToken = ""
	a.msaRefresh = ""
	a.xauToken = ""
	a.mpXstsToken = ""
	a.xblXstsToken = ""
	a.tokenExpiry = time.Time{}
	a.userCode = ""
	a.verificationURL = ""
	a.errorMessage = ""
	a.authMu.Unlock()

	if a.db != nil {
		_ = a.db.ClearAuthSession("xbox")
	}

	ws.GetHub().Broadcast("xbox:status", a.Status())
	ws.GetHub().Broadcast("services:updated", nil)
	return nil
}

// GetAccessToken returns the active Xbox Live Authorization header: XBL3.0 x={uhs};{token}
func (a *Adapter) GetAccessToken() string {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	if a.userHash == "" || a.xblXstsToken == "" {
		return ""
	}
	return fmt.Sprintf("XBL3.0 x=%s;%s", a.userHash, a.xblXstsToken)
}

// GetAccountID returns the Xbox User ID (XUID)
func (a *Adapter) GetAccountID() string {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.xuid
}

func (a *Adapter) setAuthError(msg string) {
	a.authMu.Lock()
	a.state = ports.StateError
	a.errorMessage = msg
	a.userCode = ""
	a.verificationURL = ""
	a.authMu.Unlock()

	log.Printf("[Xbox Auth] Error: %s", msg)
	ws.GetHub().Broadcast("xbox:status", a.Status())
}

// restoreSavedSession restores authentic session on startup
func (a *Adapter) restoreSavedSession() {
	if a.db == nil {
		return
	}

	session, err := a.db.GetAuthSession("xbox")
	if err != nil || session == nil || !session.IsActive || session.RefreshToken == "" {
		return
	}

	log.Printf("[Xbox Auth] Restoring saved session for Gamertag: %s...", session.Username)

	var data savedSessionData
	if session.SessionData != "" {
		_ = json.Unmarshal([]byte(session.SessionData), &data)
	}

	a.authMu.Lock()
	a.gamertag = session.Username
	a.xuid = session.AccountID
	a.msaToken = session.AccessToken
	a.msaRefresh = session.RefreshToken
	a.userHash = data.UserHash
	a.xauToken = data.XAUToken
	a.mpXstsToken = data.MpXstsToken
	a.xblXstsToken = data.XblXstsToken
	a.updateXstsToken = data.UpdateXstsToken
	a.tokenExpiry = data.TokenExpiry

	if a.userHash != "" && a.xblXstsToken != "" && time.Now().Before(a.tokenExpiry) {
		a.state = ports.StateConnected
	} else {
		a.state = ports.StateConnecting
	}
	a.authMu.Unlock()

	// Asynchronously verify or refresh tokens in background
	go func() {
		if err := a.ensureValidTokens(); err != nil {
			log.Printf("[Xbox Auth] Notice: Could not silently refresh restored session: %v", err)
			a.authMu.Lock()
			if a.xblXstsToken == "" || time.Now().After(a.tokenExpiry) {
				a.state = ports.StateDisconnected
			}
			a.authMu.Unlock()
		} else {
			a.authMu.Lock()
			a.state = ports.StateConnected
			a.authMu.Unlock()
			log.Printf("[Xbox Auth] Genuine Xbox session active for %s", session.Username)
		}
		ws.GetHub().Broadcast("xbox:status", a.Status())
	}()
}

// requestXAU tries both "d=" and "t=" ticket prefixes against user.auth.xboxlive.com
func (a *Adapter) requestXAU(msaAccessToken string) (string, string, error) {
	cleanToken := strings.TrimPrefix(strings.TrimPrefix(msaAccessToken, "d="), "t=")
	prefixes := []string{"d=", "t="}
	var lastErr error

	for _, p := range prefixes {
		ticket := p + cleanToken
		xauReqPayload := map[string]interface{}{
			"Properties": map[string]interface{}{
				"AuthMethod": "RPS",
				"SiteName":   "user.auth.xboxlive.com",
				"RpsTicket":  ticket,
			},
			"RelyingParty": "http://auth.xboxlive.com",
			"TokenType":    "JWT",
		}

		xauJSON, _ := json.Marshal(xauReqPayload)
		req, err := http.NewRequest("POST", XAUAuthenticateURL, bytes.NewReader(xauJSON))
		if err != nil {
			return "", "", fmt.Errorf("create XAU request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("x-xbl-contract-version", "1")
		req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("execute XAU request: %w", err)
			continue
		}

		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var xauResp struct {
				Token         string `json:"Token"`
				DisplayClaims struct {
					Xui []struct {
						Uhs string `json:"uhs"`
					} `json:"xui"`
				} `json:"DisplayClaims"`
			}
			if err := json.Unmarshal(b, &xauResp); err == nil && xauResp.Token != "" && len(xauResp.DisplayClaims.Xui) > 0 {
				return xauResp.DisplayClaims.Xui[0].Uhs, xauResp.Token, nil
			}
		}

		xerr := resp.Header.Get("X-Err")
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		errMsg := explainXErr(xerr)
		if errMsg == "" {
			if len(b) > 0 {
				errMsg = string(b)
			} else if xerr != "" {
				errMsg = fmt.Sprintf("X-Err: %s", xerr)
			} else if wwwAuth != "" {
				errMsg = fmt.Sprintf("WWW-Authenticate: %s", wwwAuth)
			} else {
				errMsg = "unauthorized"
			}
		}
		lastErr = fmt.Errorf("XAU exchange rejected (%d): %s", resp.StatusCode, errMsg)
	}

	return "", "", lastErr
}

func explainXErr(xerr string) string {
	switch strings.TrimSpace(xerr) {
	case "2148916233", "0x8015DC09":
		return "This Microsoft account does not have an Xbox Live profile yet. Please log into https://xbox.com once in your browser to create your Gamertag."
	case "2148916238", "0x8015DC0E":
		return "This Microsoft account is registered as a child/minor and must be added to a Microsoft Family Safety group by an adult account."
	case "2148916235", "0x8015DC0B":
		return "Xbox Live is not currently available in your account's country or region."
	case "2148916236", "0x8015DC0C":
		return "Adult verification is required for this account. Please complete verification at https://account.microsoft.com."
	case "2148916237", "0x8015DC0D":
		return "Pending Xbox Terms of Service agreement. Please log into https://xbox.com in a browser to accept the updated terms."
	default:
		return ""
	}
}
