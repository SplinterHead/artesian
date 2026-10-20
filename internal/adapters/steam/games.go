package steam

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"artesian/internal/ports"
)

type cachedPlatformMeta struct {
	Name      string
	Platforms []string
}

var (
	platformCache   = make(map[int64]cachedPlatformMeta)
	platformCacheMu sync.RWMutex
)

var (
	appLinkRegex     = regexp.MustCompile(`https?://steamcommunity\.com/app/(\d+)`)
	gameNameDivRegex = regexp.MustCompile(`(?s)href="https?://steamcommunity\.com/app/(\d+)"[^>]*>.*?<div class="game_name"><a [^>]*>([^<]+)</a>`)
	whiteLinkRegex   = regexp.MustCompile(`(?s)<div class="game_name"><a class="whiteLink" href="https?://steamcommunity\.com/app/(\d+)">([^<]+)</a>`)
)

type steamProfileXML struct {
	XMLName         xml.Name `xml:"profile"`
	SteamID64       string   `xml:"steamID64"`
	SteamID         string   `xml:"steamID"`
	CustomURL       string   `xml:"customURL"`
	MostPlayedGames []struct {
		GameName  string `xml:"gameName"`
		GameLink  string `xml:"gameLink"`
		StatsName string `xml:"statsName"`
	} `xml:"mostPlayedGames>mostPlayedGame"`
}

type steamGamesListXML struct {
	XMLName   xml.Name `xml:"gamesList"`
	SteamID64 string   `xml:"steamID64"`
	Games     []struct {
		AppID int64  `xml:"appID"`
		Name  string `xml:"name"`
	} `xml:"games>game"`
}

type steamWebAPIOwnedGamesResp struct {
	Response struct {
		GameCount int `json:"game_count"`
		Games     []struct {
			AppID int64  `json:"appid"`
			Name  string `json:"name"`
		} `json:"games"`
	} `json:"response"`
}

// GetOwnedGames discovers and resolves games for the authenticated Steam user
func (a *Adapter) GetOwnedGames() (*ports.AccountGamesResult, error) {
	a.authMu.RLock()
	username := a.username
	steamID64 := a.steamID
	accessToken := a.accessToken
	sessionCookies := a.sessionCookies
	a.authMu.RUnlock()

	if (username == "" || steamID64 == "" || accessToken == "") && a.db != nil {
		sess, err := a.db.GetAuthSession(a.ID())
		if err == nil && sess != nil {
			if username == "" {
				username = sess.Username
			}
			if steamID64 == "" {
				steamID64 = sess.AccountID
			}
			if accessToken == "" {
				accessToken = sess.AccessToken
			}
			if sessionCookies == "" {
				sessionCookies = sess.SessionData
			}
		}
	}
	if username == "" {
		return &ports.AccountGamesResult{Games: []ports.AccountGame{}}, fmt.Errorf("no connected Steam account found")
	}

	httpClient := &http.Client{Timeout: 12 * time.Second}
	discovered := make(map[int64]string) // appID -> name
	fullLibraryLoaded := false

	if steamID64 == "" && len(username) == 17 && strings.HasPrefix(username, "7656119") {
		steamID64 = username
	}

	// 1. If SteamID64 is not yet known, resolve from Community Profile XML
	if steamID64 == "" {
		profileXMLURL := fmt.Sprintf("https://steamcommunity.com/id/%s/?xml=1", username)
		if req, err := http.NewRequest("GET", profileXMLURL, nil); err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var p steamProfileXML
				if body, err := io.ReadAll(resp.Body); err == nil {
					if err := xml.Unmarshal(body, &p); err == nil && p.SteamID64 != "" {
						steamID64 = p.SteamID64
						a.authMu.Lock()
						if a.steamID == "" {
							a.steamID = p.SteamID64
						}
						a.authMu.Unlock()
					}
				}
			}
		}
	}

	// 2. Authenticated Steam Web API: IPlayerService/GetOwnedGames (OAuth access_token)
	if accessToken != "" && !strings.HasPrefix(accessToken, "st_token_") && steamID64 != "" {
		apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?access_token=%s&steamid=%s&include_appinfo=1&include_played_free_games=1", accessToken, steamID64)
		if req, err := http.NewRequest("GET", apiURL, nil); err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var ownedResp steamWebAPIOwnedGamesResp
				if err := json.NewDecoder(resp.Body).Decode(&ownedResp); err == nil && len(ownedResp.Response.Games) > 0 {
					fullLibraryLoaded = true
					for _, g := range ownedResp.Response.Games {
						if g.AppID > 0 {
							name := strings.TrimSpace(g.Name)
							discovered[g.AppID] = name
						}
					}
					log.Printf("[Steam Library] Authenticated IPlayerService returned %d owned games for %s", len(ownedResp.Response.Games), username)
				}
			}
		}
	}

	// 3. Authenticated Steam Community XML with session cookies (steamLoginSecure)
	if !fullLibraryLoaded && sessionCookies != "" {
		targetProfile := steamID64
		if targetProfile == "" {
			targetProfile = username
		}
		gamesXMLURL := fmt.Sprintf("https://steamcommunity.com/profiles/%s/games/?xml=1", targetProfile)
		if !strings.HasPrefix(targetProfile, "7656119") {
			gamesXMLURL = fmt.Sprintf("https://steamcommunity.com/id/%s/games/?xml=1", targetProfile)
		}
		if req, err := http.NewRequest("GET", gamesXMLURL, nil); err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			req.Header.Set("Cookie", sessionCookies)
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var gl steamGamesListXML
				if body, err := io.ReadAll(resp.Body); err == nil {
					if err := xml.Unmarshal(body, &gl); err == nil && len(gl.Games) > 0 {
						fullLibraryLoaded = true
						for _, g := range gl.Games {
							if g.AppID > 0 {
								name := strings.TrimSpace(g.Name)
								discovered[g.AppID] = name
							}
						}
						log.Printf("[Steam Library] Authenticated Community XML returned %d games for %s", len(gl.Games), username)
					}
				}
			}
		}
	}

	// 4. Authenticated Store Userdata (Discovers any additional owned store licenses)
	if sessionCookies != "" {
		if req, err := http.NewRequest("GET", "https://store.steampowered.com/dynamicstore/userdata/", nil); err == nil {
			req.Header.Set("Cookie", sessionCookies)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var ud struct {
					RgOwnedApps []int64 `json:"rgOwnedApps"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&ud); err == nil && len(ud.RgOwnedApps) > 0 {
					for _, appID := range ud.RgOwnedApps {
						if appID > 0 {
							if _, exists := discovered[appID]; !exists {
								discovered[appID] = ""
							}
						}
					}
				}
			}
		}
	}

	// 5. Developer Web API GetOwnedGames (if API key available)
	if !fullLibraryLoaded {
		apiKey := os.Getenv("STEAM_API_KEY")
		if apiKey == "" && a.db != nil {
			apiKey = a.db.GetSetting("steam_api_key", "")
		}
		if apiKey != "" && steamID64 != "" {
			apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1&include_played_free_games=1", apiKey, steamID64)
			if req, err := http.NewRequest("GET", apiURL, nil); err == nil {
				if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
					defer resp.Body.Close()
					var ownedResp steamWebAPIOwnedGamesResp
					if err := json.NewDecoder(resp.Body).Decode(&ownedResp); err == nil && len(ownedResp.Response.Games) > 0 {
						fullLibraryLoaded = true
						for _, g := range ownedResp.Response.Games {
							if g.AppID > 0 {
								name := strings.TrimSpace(g.Name)
								discovered[g.AppID] = name
							}
						}
					}
				}
			}
		}
	}

	// 6. Public Community Fallback (XML & HTML Scraping)
	if !fullLibraryLoaded {
		// Public Games List XML (if user made Game Details public)
		gamesXMLURL := fmt.Sprintf("https://steamcommunity.com/id/%s/games/?xml=1", username)
		if steamID64 != "" {
			gamesXMLURL = fmt.Sprintf("https://steamcommunity.com/profiles/%s/games/?xml=1", steamID64)
		}
		if req, err := http.NewRequest("GET", gamesXMLURL, nil); err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var gl steamGamesListXML
				if body, err := io.ReadAll(resp.Body); err == nil {
					if err := xml.Unmarshal(body, &gl); err == nil && len(gl.Games) > 0 {
						fullLibraryLoaded = true
						for _, g := range gl.Games {
							if g.AppID > 0 {
								name := strings.TrimSpace(g.Name)
								discovered[g.AppID] = name
							}
						}
					}
				}
			}
		}

		// Public Profile HTML: recent games, showcases, badges
		profileHTMLURL := fmt.Sprintf("https://steamcommunity.com/id/%s/", username)
		if steamID64 != "" {
			profileHTMLURL = fmt.Sprintf("https://steamcommunity.com/profiles/%s/", steamID64)
		}
		if req, err := http.NewRequest("GET", profileHTMLURL, nil); err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Artesian/1.0")
			if resp, err := httpClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				if body, err := io.ReadAll(resp.Body); err == nil {
					html := string(body)
					divMatches := gameNameDivRegex.FindAllStringSubmatch(html, -1)
					for _, match := range divMatches {
						if len(match) > 2 {
							appID, _ := strconv.ParseInt(match[1], 10, 64)
							name := strings.TrimSpace(match[2])
							if appID > 0 {
								discovered[appID] = name
							}
						}
					}
					linkMatches := whiteLinkRegex.FindAllStringSubmatch(html, -1)
					for _, match := range linkMatches {
						if len(match) > 2 {
							appID, _ := strconv.ParseInt(match[1], 10, 64)
							name := strings.TrimSpace(match[2])
							if appID > 0 && discovered[appID] == "" {
								discovered[appID] = name
							}
						}
					}
				}
			}
		}
	}

	// 7. Check local database (depot_meta_cache, watched_apps, game_meta_cache)
	if a.db != nil {
		a.loadLocalSteamGames(discovered)
	}

	// 8. Resolve Names & Platforms efficiently
	var (
		mu      sync.Mutex
		results []ports.AccountGame
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 8)
	)

	needResolveCount := 0

	for appID, name := range discovered {
		if appID <= 0 {
			continue
		}

		// Check fast in-memory platform cache
		platformCacheMu.RLock()
		cached, ok := platformCache[appID]
		platformCacheMu.RUnlock()
		if ok && len(cached.Platforms) > 0 {
			n := cached.Name
			if n == "" {
				n = name
			}
			results = append(results, ports.AccountGame{
				AppID:     appID,
				Service:   "steam",
				Name:      n,
				Platforms: cached.Platforms,
			})
			continue
		}

		// Check DB game_meta_cache
		if a.db != nil {
			dbName, dbPlatform, err := a.db.GetGameMeta(fmt.Sprintf("steam_%d", appID))
			if err == nil && dbPlatform != "" {
				platforms := parsePlatformsString(dbPlatform)
				n := dbName
				if n == "" {
					n = name
				}
				results = append(results, ports.AccountGame{
					AppID:     appID,
					Service:   "steam",
					Name:      n,
					Platforms: platforms,
				})
				continue
			}
		}

		// For large libraries (> 30 games), resolve the first 25 uncached and default remainder
		// to Windows so modal opens instantaneously; platforms are resolved live when game is clicked
		if len(discovered) > 30 && needResolveCount > 25 {
			dispName := name
			if dispName == "" {
				dispName = fmt.Sprintf("Steam App %d", appID)
			}
			results = append(results, ports.AccountGame{
				AppID:     appID,
				Service:   "steam",
				Name:      dispName,
				Platforms: []string{"Windows"},
			})
			continue
		}

		needResolveCount++
		wg.Add(1)
		go func(id int64, currName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resolvedName, platforms := a.resolveAppPlatforms(id, currName, httpClient)
			if resolvedName == "" {
				resolvedName = fmt.Sprintf("Steam App %d", id)
			}
			if len(platforms) == 0 {
				platforms = []string{"Windows"}
			}

			mu.Lock()
			results = append(results, ports.AccountGame{
				AppID:     id,
				Service:   "steam",
				Name:      resolvedName,
				Platforms: platforms,
			})
			mu.Unlock()

			if a.db != nil {
				_ = a.db.UpsertGameMeta(
					fmt.Sprintf("steam_%d", id),
					"steam",
					id,
					resolvedName,
					strings.Join(platforms, ", "),
				)
			}
		}(appID, name)
	}

	wg.Wait()

	// 9. Sort alphabetically by name
	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	privacyRestricted := !fullLibraryLoaded
	var hint string
	if privacyRestricted {
		hint = "Only recent games are visible because your Steam Profile 'Game Details' is set to Friends Only. Connect with your Steam Account in Accounts & Modules to load your full library directly."
	}

	log.Printf("[Steam Games] Built dynamic game list for %s: found %d games (full_library: %v)", username, len(results), fullLibraryLoaded)
	return &ports.AccountGamesResult{
		Games:             results,
		TotalFound:        len(results),
		PrivacyRestricted: privacyRestricted,
		Hint:              hint,
	}, nil
}

// ResolveAppPlatforms resolves game name and platform capabilities on demand
func (a *Adapter) ResolveAppPlatforms(appID int64, currentName string) (string, []string) {
	return a.resolveAppPlatforms(appID, currentName, a.httpClient)
}

func (a *Adapter) resolveAppPlatforms(appID int64, currentName string, client *http.Client) (string, []string) {
	// 1. Check in-memory cache
	platformCacheMu.RLock()
	cached, ok := platformCache[appID]
	platformCacheMu.RUnlock()
	if ok && len(cached.Platforms) > 0 {
		name := cached.Name
		if name == "" {
			name = currentName
		}
		return name, cached.Platforms
	}

	// 2. Check DB game_meta_cache
	if a.db != nil {
		dbName, dbPlatform, err := a.db.GetGameMeta(fmt.Sprintf("steam_%d", appID))
		if err == nil && dbPlatform != "" {
			platforms := parsePlatformsString(dbPlatform)
			name := dbName
			if name == "" {
				name = currentName
			}
			platformCacheMu.Lock()
			platformCache[appID] = cachedPlatformMeta{Name: name, Platforms: platforms}
			platformCacheMu.Unlock()
			return name, platforms
		}
	}

	// 3. Query Steam Store Web API
	storeURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d&filters=basic,platforms", appID)
	req, err := http.NewRequest("GET", storeURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var storeResp map[string]struct {
				Success bool `json:"success"`
				Data    struct {
					Type      string `json:"type"`
					Name      string `json:"name"`
					Platforms struct {
						Windows bool `json:"windows"`
						Mac     bool `json:"mac"`
						Linux   bool `json:"linux"`
					} `json:"platforms"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&storeResp); err == nil {
				if entry, ok := storeResp[strconv.FormatInt(appID, 10)]; ok && entry.Success {
					name := strings.TrimSpace(entry.Data.Name)
					if name == "" {
						name = currentName
					}
					var platforms []string
					if entry.Data.Platforms.Windows {
						platforms = append(platforms, "Windows")
					}
					if entry.Data.Platforms.Mac {
						platforms = append(platforms, "Mac")
					}
					if entry.Data.Platforms.Linux {
						platforms = append(platforms, "Linux")
					}
					if len(platforms) == 0 {
						platforms = []string{"Windows"}
					}

					platformCacheMu.Lock()
					platformCache[appID] = cachedPlatformMeta{Name: name, Platforms: platforms}
					platformCacheMu.Unlock()
					return name, platforms
				}
			}
		}
	}

	// Fallback
	name := currentName
	if name == "" {
		name = fmt.Sprintf("Steam App %d", appID)
	}
	platforms := []string{"Windows"}
	platformCacheMu.Lock()
	platformCache[appID] = cachedPlatformMeta{Name: name, Platforms: platforms}
	platformCacheMu.Unlock()
	return name, platforms
}

func parsePlatformsString(s string) []string {
	if s == "" || s == "PC" {
		return []string{"Windows"}
	}
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	if len(res) == 0 {
		return []string{"Windows"}
	}
	return res
}

func (a *Adapter) loadLocalSteamGames(discovered map[int64]string) {
	if a.db == nil {
		return
	}

	// Query depot_meta_cache
	if rows, err := a.db.Query("SELECT DISTINCT app_id, game_title FROM depot_meta_cache WHERE service = 'steam' AND app_id > 0"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var appID int64
			var title string
			if err := rows.Scan(&appID, &title); err == nil && appID > 0 {
				if discovered[appID] == "" {
					discovered[appID] = strings.TrimSpace(title)
				}
			}
		}
	}

	// Query watched_apps
	if rows, err := a.db.Query("SELECT DISTINCT app_id, name FROM watched_apps WHERE service = 'steam' AND app_id > 0"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var appID int64
			var name string
			if err := rows.Scan(&appID, &name); err == nil && appID > 0 {
				if discovered[appID] == "" {
					discovered[appID] = strings.TrimSpace(name)
				}
			}
		}
	}

	// Query game_meta_cache
	if rows, err := a.db.Query("SELECT DISTINCT app_id, name FROM game_meta_cache WHERE service = 'steam' AND app_id > 0"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var appID int64
			var name string
			if err := rows.Scan(&appID, &name); err == nil && appID > 0 {
				if discovered[appID] == "" {
					discovered[appID] = strings.TrimSpace(name)
				}
			}
		}
	}
}
