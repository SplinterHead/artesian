package epic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

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
	var uncachedItems []pendingItem
	var discoveredGames []ports.AccountGame

	// Check existing in-memory cache and SQLite cache first to avoid redundant network lookups
	var knownInfos []*EpicGameInfo

	a.gamesMu.RLock()
	for _, item := range assets {
		if strings.EqualFold(item.Namespace, "ue") || item.AppName == "" || item.CatalogItemID == "" {
			continue
		}
		appID := HashAppName(item.AppName)
		if cached, exists := a.gamesCache[appID]; exists && cached.Title != "" {
			discoveredGames = append(discoveredGames, ports.AccountGame{
				AppID:     appID,
				Service:   a.ID(),
				Name:      cached.Title,
				Platforms: cached.Platforms,
			})
			continue
		}
		// Check SQLite game_meta_cache
		if a.db != nil {
			if name, platStr, err := a.db.GetGameMeta(fmt.Sprintf("epic_%d", appID)); err == nil && name != "" {
				var plats []string
				if platStr != "" {
					for _, p := range strings.Split(platStr, ",") {
						if trimmed := strings.TrimSpace(p); trimmed != "" {
							plats = append(plats, trimmed)
						}
					}
				}
				if len(plats) == 0 {
					plats = []string{"Windows"}
				}
				discoveredGames = append(discoveredGames, ports.AccountGame{
					AppID:     appID,
					Service:   a.ID(),
					Name:      name,
					Platforms: plats,
				})
				knownInfos = append(knownInfos, &EpicGameInfo{
					AppID:         appID,
					AppName:       item.AppName,
					CatalogItemID: item.CatalogItemID,
					Namespace:     item.Namespace,
					Title:         name,
					BuildVersion:  item.BuildVersion,
					Platforms:     plats,
				})
				continue
			}
		}

		uncachedItems = append(uncachedItems, pendingItem{
			appName:       item.AppName,
			catalogItemID: item.CatalogItemID,
			namespace:     item.Namespace,
			buildVersion:  item.BuildVersion,
		})
	}
	a.gamesMu.RUnlock()

	if len(knownInfos) > 0 {
		a.gamesMu.Lock()
		for _, info := range knownInfos {
			a.gamesCache[info.AppID] = info
			a.gamesByName[strings.ToLower(info.AppName)] = info
			a.gamesByName[strings.ToLower(info.Title)] = info
		}
		a.gamesMu.Unlock()
	}

	// 3. Resolve uncached titles from Epic Catalog bulk API concurrently
	if len(uncachedItems) > 0 {
		itemsByNamespace := make(map[string][]pendingItem)
		for _, it := range uncachedItems {
			itemsByNamespace[it.namespace] = append(itemsByNamespace[it.namespace], it)
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, 8) // up to 8 concurrent catalog requests
		var mu sync.Mutex
		newInfos := make([]*EpicGameInfo, 0, len(uncachedItems))

		for ns, items := range itemsByNamespace {
			for i := 0; i < len(items); i += 50 {
				end := i + 50
				if end > len(items) {
					end = len(items)
				}
				chunk := items[i:end]
				namespace := ns

				wg.Add(1)
				go func(nsName string, chunkItems []pendingItem) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					values := url.Values{}
					for _, it := range chunkItems {
						values.Add("id", it.catalogItemID)
					}
					values.Set("includeMainGameDetails", "true")
					values.Set("country", "GB")
					values.Set("locale", "en")

					catalogURL := fmt.Sprintf("https://catalog-public-service-prod06.ol.epicgames.com/catalog/api/shared/namespace/%s/bulk/items?%s",
						url.PathEscape(nsName), values.Encode())

					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
					defer cancel()

					var catalogMap map[string]catalogItemDetails

					cReq, cErr := http.NewRequestWithContext(ctx, "GET", catalogURL, nil)
					if cErr == nil {
						cReq.Header.Set("Authorization", "Bearer "+token)
						cReq.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")

						cResp, doErr := a.httpClient.Do(cReq)
						if doErr == nil && cResp.StatusCode == http.StatusOK {
							cBytes, _ := io.ReadAll(cResp.Body)
							_ = cResp.Body.Close()
							_ = json.Unmarshal(cBytes, &catalogMap)
						} else if cResp != nil {
							_ = cResp.Body.Close()
						}
					}

					for _, it := range chunkItems {
						title := it.appName
						platforms := []string{"Windows"}

						if catalogMap != nil {
							if details, found := catalogMap[it.catalogItemID]; found && details.Title != "" {
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
						}

						appID := HashAppName(it.appName)
						info := &EpicGameInfo{
							AppID:         appID,
							AppName:       it.appName,
							CatalogItemID: it.catalogItemID,
							Namespace:     it.namespace,
							Title:         title,
							BuildVersion:  it.buildVersion,
							Platforms:     platforms,
						}

						mu.Lock()
						newInfos = append(newInfos, info)
						mu.Unlock()
					}
				}(namespace, chunk)
			}
		}
		wg.Wait()

		// Batch update caches and SQLite under write lock
		a.gamesMu.Lock()
		for _, info := range newInfos {
			a.gamesCache[info.AppID] = info
			a.gamesByName[strings.ToLower(info.AppName)] = info

			if a.db != nil {
				_ = a.db.UpsertGameMeta(
					fmt.Sprintf("epic_%d", info.AppID),
					a.ID(),
					info.AppID,
					info.Title,
					strings.Join(info.Platforms, ", "),
				)
			}

			discoveredGames = append(discoveredGames, ports.AccountGame{
				AppID:     info.AppID,
				Service:   a.ID(),
				Name:      info.Title,
				Platforms: info.Platforms,
			})
		}
		a.gamesMu.Unlock()
	}

	// Sort games alphabetically by name
	sort.Slice(discoveredGames, func(i, j int) bool {
		return strings.ToLower(discoveredGames[i].Name) < strings.ToLower(discoveredGames[j].Name)
	})

	a.authMu.RLock()
	username := a.username
	a.authMu.RUnlock()
	if username == "" {
		username = "user"
	}

	log.Printf("[Epic Library] Retrieved %d owned games from genuine library for %s", len(discoveredGames), username)

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
