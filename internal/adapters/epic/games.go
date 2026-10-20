package epic

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"artesian/internal/ports"
)

type launcherAssetItem struct {
	AppName       string `json:"appName"`
	CatalogItemID string `json:"catalogItemId"`
	Namespace     string `json:"namespace"`
	AssetID       string `json:"assetId"`
	BuildVersion  string `json:"buildVersion"`
	LabelName     string `json:"labelName"`
}

type catalogItemDetails struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	KeyImages   []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"keyImages"`
	ReleaseInfo []struct {
		AppID    string   `json:"appId"`
		Platform []string `json:"platform"`
	} `json:"releaseInfo"`
}

// GetOwnedGames discovers genuine owned games via Epic Games Launcher and Catalog services
func (a *Adapter) GetOwnedGames() (*ports.AccountGamesResult, error) {
	token := a.GetAccessToken()
	if token == "" {
		return nil, fmt.Errorf("epic games account not authenticated")
	}

	// 1. Fetch playable assets for Windows
	assetsURL := "https://launcher-public-service-prod06.ol.epicgames.com/launcher/api/public/assets/Windows?label=Live"
	req, err := http.NewRequest("GET", assetsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch epic assets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Attempt one refresh
		a.authMu.Lock()
		_ = a.refreshAccessTokenLocked()
		token = a.accessToken
		a.authMu.Unlock()

		req.Header.Set("Authorization", "Bearer "+token)
		resp2, err2 := a.httpClient.Do(req)
		if err2 != nil || resp2.StatusCode != http.StatusOK {
			if resp2 != nil {
				_ = resp2.Body.Close()
			}
			return nil, fmt.Errorf("unauthorized response from epic launcher service")
		}
		resp = resp2
		defer resp.Body.Close()
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read epic assets response: %w", err)
	}

	var assets []launcherAssetItem
	if err := json.Unmarshal(bodyBytes, &assets); err != nil {
		return nil, fmt.Errorf("failed to decode epic assets: %w", err)
	}

	// 2. Filter out Unreal Engine marketplace assets (namespace == "ue")
	type pendingItem struct {
		appName       string
		catalogItemID string
		namespace     string
		buildVersion  string
	}
	itemsByNamespace := make(map[string][]pendingItem)

	for _, item := range assets {
		if strings.EqualFold(item.Namespace, "ue") || item.AppName == "" || item.CatalogItemID == "" {
			continue
		}
		itemsByNamespace[item.Namespace] = append(itemsByNamespace[item.Namespace], pendingItem{
			appName:       item.AppName,
			catalogItemID: item.CatalogItemID,
			namespace:     item.Namespace,
			buildVersion:  item.BuildVersion,
		})
	}

	// 3. Resolve human-readable titles from Epic Catalog bulk API
	var discoveredGames []ports.AccountGame
	a.gamesMu.Lock()
	defer a.gamesMu.Unlock()

	for ns, items := range itemsByNamespace {
		// Batch catalog items in chunks of 50
		for i := 0; i < len(items); i += 50 {
			end := i + 50
			if end > len(items) {
				end = len(items)
			}
			chunk := items[i:end]

			values := url.Values{}
			for _, it := range chunk {
				values.Add("id", it.catalogItemID)
			}
			values.Set("includeMainGameDetails", "true")
			values.Set("country", "GB")
			values.Set("locale", "en")

			catalogURL := fmt.Sprintf("https://catalog-public-service-prod06.ol.epicgames.com/catalog/api/shared/namespace/%s/bulk/items?%s",
				url.PathEscape(ns), values.Encode())

			cReq, cErr := http.NewRequest("GET", catalogURL, nil)
			if cErr != nil {
				continue
			}
			cReq.Header.Set("Authorization", "Bearer "+token)
			cReq.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")

			cResp, cErr := a.httpClient.Do(cReq)
			if cErr != nil || cResp.StatusCode != http.StatusOK {
				if cResp != nil {
					_ = cResp.Body.Close()
				}
				continue
			}

			cBytes, _ := io.ReadAll(cResp.Body)
			_ = cResp.Body.Close()

			var catalogMap map[string]catalogItemDetails
			if err := json.Unmarshal(cBytes, &catalogMap); err != nil {
				continue
			}

			for _, it := range chunk {
				details, found := catalogMap[it.catalogItemID]
				title := it.appName
				platforms := []string{"Windows"}

				if found && details.Title != "" {
					title = details.Title
					var plats []string
					for _, r := range details.ReleaseInfo {
						for _, p := range r.Platform {
							if strings.Contains(strings.ToLower(p), "win") && !containsString(plats, "Windows") {
								plats = append(plats, "Windows")
							} else if strings.Contains(strings.ToLower(p), "mac") && !containsString(plats, "Mac") {
								plats = append(plats, "Mac")
							}
						}
					}
					if len(plats) > 0 {
						platforms = plats
					}
				}

				appID := HashAppName(it.appName)
				gameInfo := &EpicGameInfo{
					AppID:         appID,
					AppName:       it.appName,
					CatalogItemID: it.catalogItemID,
					Namespace:     it.namespace,
					Title:         title,
					BuildVersion:  it.buildVersion,
					Platforms:     platforms,
				}

				a.gamesCache[appID] = gameInfo
				a.gamesByName[strings.ToLower(it.appName)] = gameInfo

				// Cache in SQLite game_meta_cache
				if a.db != nil {
					_ = a.db.UpsertGameMeta(
						fmt.Sprintf("epic_%d", appID),
						a.ID(),
						appID,
						title,
						strings.Join(platforms, ", "),
					)
				}

				discoveredGames = append(discoveredGames, ports.AccountGame{
					AppID:     appID,
					Service:   a.ID(),
					Name:      title,
					Platforms: platforms,
				})
			}
		}
	}

	log.Printf("[Epic Games] Retrieved %d owned games from genuine library", len(discoveredGames))

	return &ports.AccountGamesResult{
		Games:             discoveredGames,
		TotalFound:        len(discoveredGames),
		PrivacyRestricted: false,
	}, nil
}

// ResolveAppPlatforms returns the supported platforms and title for an Epic game
func (a *Adapter) ResolveAppPlatforms(appID int64, defaultName string) (string, []string) {
	a.gamesMu.RLock()
	info, ok := a.gamesCache[appID]
	a.gamesMu.RUnlock()

	if ok && info != nil {
		name := info.Title
		if name == "" {
			name = defaultName
		}
		if len(info.Platforms) > 0 {
			return name, info.Platforms
		}
		return name, []string{"Windows"}
	}

	// Check SQLite game_meta_cache
	if a.db != nil {
		dbName, dbPlatform, err := a.db.GetGameMeta(fmt.Sprintf("epic_%d", appID))
		if err == nil && dbName != "" {
			var plats []string
			for _, p := range strings.Split(dbPlatform, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					plats = append(plats, trimmed)
				}
			}
			if len(plats) == 0 {
				plats = []string{"Windows"}
			}
			return dbName, plats
		}
	}

	return defaultName, []string{"Windows"}
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}
