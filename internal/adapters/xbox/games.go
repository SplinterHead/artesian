package xbox

import (
	"bytes"
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

	"artesian/internal/db"
	"artesian/internal/ports"
)

const (
	CollectionsQueryURL   = "https://collections.mp.microsoft.com/v8.0/collections/b2bLicensePreview"
	DisplayCatalogURL     = "https://displaycatalog.mp.microsoft.com/v7.0/products"
	PCGamePassSiglID      = "fdd9e2a7-0fee-49f6-ad69-4354098401ff"
	ConsoleGamePassSiglID = "f6f1f99f-9b49-4ccd-b3bf-4d9767a77f5e"
)

// GetOwnedGames enumerates owned digital library titles (personal library by default)
func (a *Adapter) GetOwnedGames() (*ports.AccountGamesResult, error) {
	return a.GetOwnedGamesFiltered(false)
}

// GetOwnedGamesFiltered enumerates games, optionally including the full Game Pass catalog
func (a *Adapter) GetOwnedGamesFiltered(includeGamePass bool) (*ports.AccountGamesResult, error) {
	if err := a.ensureValidTokens(); err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	a.authMu.RLock()
	uhs := a.userHash
	mpToken := a.mpXstsToken
	xblToken := a.xblXstsToken
	xuid := a.xuid
	a.authMu.RUnlock()

	if uhs == "" || (mpToken == "" && xblToken == "") {
		return nil, fmt.Errorf("Xbox account is not authenticated")
	}

	hasGamePass := a.checkGamePassSubscription(uhs, mpToken, xblToken, xuid)
	log.Printf("[Xbox Library] Account Game Pass subscription detected: %t", hasGamePass)

	allProductIDs := make(map[string]bool)

	// 1. Query TitleHub (Personal library: played & owned games on account)
	if xuid != "" && xblToken != "" {
		titleProductIDs := a.queryTitleHistory(uhs, xblToken, xuid)
		for _, id := range titleProductIDs {
			allProductIDs[id] = true
		}
		log.Printf("[Xbox Library] Found %d products from personal TitleHub history", len(titleProductIDs))
	}

	// 2. Query Owned Digital Library (Collections API permanent licenses)
	if mpToken != "" {
		ownedIDs, err := a.queryCollections(uhs, mpToken)
		if err != nil {
			log.Printf("[Xbox Library] Notice: Collections query error: %v", err)
		} else {
			for _, id := range ownedIDs {
				allProductIDs[id] = true
			}
			log.Printf("[Xbox Library] Found %d owned digital products in account collections", len(ownedIDs))
		}
	}

	// 3. Query Game Pass Catalog (only if requested via includeGamePass)
	if includeGamePass {
		gpIDs := a.queryGamePassCatalog()
		for _, id := range gpIDs {
			allProductIDs[id] = true
		}
		log.Printf("[Xbox Library] Included %d catalog products from Game Pass", len(gpIDs))
	}

	if len(allProductIDs) == 0 {
		return &ports.AccountGamesResult{
			Games:       []ports.AccountGame{},
			TotalFound:  0,
			HasGamePass: hasGamePass,
		}, nil
	}

	// 4. Hydrate product metadata & cover info via Display Catalog
	var idList []string
	for id := range allProductIDs {
		idList = append(idList, id)
	}

	hydratedGames := a.hydrateProducts(idList)

	// Sort alphabetically by title
	sort.Slice(hydratedGames, func(i, j int) bool {
		return strings.ToLower(hydratedGames[i].Name) < strings.ToLower(hydratedGames[j].Name)
	})

	return &ports.AccountGamesResult{
		Games:       hydratedGames,
		TotalFound:  len(hydratedGames),
		HasGamePass: hasGamePass,
	}, nil
}

// checkGamePassSubscription checks if the account has an active Xbox Game Pass subscription
func (a *Adapter) checkGamePassSubscription(uhs, mpToken, xblToken, xuid string) bool {
	// 1. Check AccountTier via profile.xboxlive.com
	if xblToken != "" && xuid != "" {
		profileURL := fmt.Sprintf("https://profile.xboxlive.com/users/xuid(%s)/profile/settings?settings=AccountTier", xuid)
		req, err := http.NewRequest("GET", profileURL, nil)
		if err == nil {
			req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xblToken))
			req.Header.Set("x-xbl-contract-version", "2")
			req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

			resp, err := a.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var profResp struct {
						ProfileUsers []struct {
							Settings []struct {
								ID    string `json:"id"`
								Value string `json:"value"`
							} `json:"settings"`
						} `json:"profileUsers"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&profResp); err == nil {
						for _, u := range profResp.ProfileUsers {
							for _, s := range u.Settings {
								if strings.EqualFold(s.ID, "AccountTier") {
									if strings.EqualFold(s.Value, "Gold") {
										return true
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 2. Check active Game Pass entitlement licenses via b2bLicensePreview
	if mpToken != "" {
		gamePassProductIDs := []map[string]string{
			{"productId": "CFQ7TTC0KGQ8"}, // PC Game Pass
			{"productId": "CFQ7TTC0KHS0"}, // Xbox Game Pass Ultimate
			{"productId": "CFQ7TTC0K6L8"}, // Xbox Game Pass Core
			{"productId": "CFQ7TTC0K5DH"}, // Xbox Game Pass Console / Standard
			{"productId": "9P1WXPKB65GQ"}, // EA Play
		}
		reqBody := map[string]interface{}{
			"maxPageSize":           20,
			"excludeDuplicated":     true,
			"expandSatisfyingItems": true,
			"market":                "neutral",
			"productSkuIds":         gamePassProductIDs,
		}
		jsonBody, _ := json.Marshal(reqBody)
		req, err := http.NewRequest("POST", CollectionsQueryURL, bytes.NewReader(jsonBody))
		if err == nil {
			req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", uhs, mpToken))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

			resp, err := a.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var colResp struct {
						Items []struct {
							ProductID string `json:"productId"`
						} `json:"items"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&colResp); err == nil && len(colResp.Items) > 0 {
						return true
					}
				}
			}
		}
	}

	return false
}

// queryTitleHistory queries https://titlehub.xboxlive.com/users/xuid({xuid})/titles/titlehistory/decoration/scid,image,detail
func (a *Adapter) queryTitleHistory(uhs, xblToken, xuid string) []string {
	reqURL := fmt.Sprintf("https://titlehub.xboxlive.com/users/xuid(%s)/titles/titlehistory/decoration/scid,image,detail", xuid)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xblToken))
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("x-xbl-contract-version", "2")
	req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		log.Printf("[Xbox Library] Notice: TitleHub request error: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("[Xbox Library] Notice: TitleHub returned %d: %s", resp.StatusCode, string(b))
		return nil
	}

	var thResp struct {
		Titles []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Detail *struct {
				Availabilities []struct {
					ProductID string `json:"ProductId"`
				} `json:"availabilities"`
			} `json:"detail"`
		} `json:"titles"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&thResp); err != nil {
		log.Printf("[Xbox Library] Notice: Failed to decode TitleHub response: %v", err)
		return nil
	}

	var prodIDs []string
	for _, t := range thResp.Titles {
		if strings.ToLower(t.Type) != "game" {
			continue
		}
		if t.Detail != nil {
			for _, av := range t.Detail.Availabilities {
				pid := strings.TrimSpace(av.ProductID)
				if pid != "" {
					prodIDs = append(prodIDs, pid)
					break
				}
			}
		}
	}
	return prodIDs
}

// queryCollections queries https://collections.mp.microsoft.com/v8.0/collections/b2bLicensePreview
func (a *Adapter) queryCollections(uhs, mpToken string) ([]string, error) {
	var productIDs []string
	continuationToken := ""

	for {
		reqBody := map[string]interface{}{
			"maxPageSize":           200,
			"excludeDuplicated":     true,
			"expandSatisfyingItems": true,
			"market":                "neutral",
			"entitlementFilters":    []string{"*:Game"},
		}
		if continuationToken != "" {
			reqBody["continuationToken"] = continuationToken
		}
		jsonBody, _ := json.Marshal(reqBody)

		req, err := http.NewRequest("POST", CollectionsQueryURL, bytes.NewReader(jsonBody))
		if err != nil {
			return productIDs, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", uhs, mpToken))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return productIDs, err
		}

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return productIDs, fmt.Errorf("collections query rejected (%d): %s", resp.StatusCode, string(b))
		}

		var colResp struct {
			Items []struct {
				ProductID string `json:"productId"`
			} `json:"items"`
			ContinuationToken string `json:"continuationToken"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&colResp); err != nil {
			resp.Body.Close()
			return productIDs, err
		}
		resp.Body.Close()

		for _, it := range colResp.Items {
			id := strings.TrimSpace(it.ProductID)
			if id != "" {
				productIDs = append(productIDs, id)
			}
		}

		continuationToken = strings.TrimSpace(colResp.ContinuationToken)
		if continuationToken == "" || len(colResp.Items) == 0 {
			break
		}
	}

	return productIDs, nil
}

// queryGamePassCatalog fetches active PC & Console Game Pass game lists without auth
func (a *Adapter) queryGamePassCatalog() []string {
	var results []string
	sigls := []string{PCGamePassSiglID, ConsoleGamePassSiglID}

	for _, sigl := range sigls {
		reqURL := fmt.Sprintf("https://catalog.gamepass.com/sigls/v2?id=%s&language=en-us&market=US", sigl)
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var items []struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&items); err == nil {
			for _, it := range items {
				id := strings.TrimSpace(it.ID)
				if id != "" {
					results = append(results, id)
				}
			}
		}
		resp.Body.Close()
	}
	return results
}

// hydrateProducts queries Display Catalog in concurrent batches of up to 100 IDs
func (a *Adapter) hydrateProducts(productIDs []string) []ports.AccountGame {
	batchSize := 100
	var batches [][]string
	for i := 0; i < len(productIDs); i += batchSize {
		end := i + batchSize
		if end > len(productIDs) {
			end = len(productIDs)
		}
		batches = append(batches, productIDs[i:end])
	}

	var mu sync.Mutex
	var games []ports.AccountGame

	// Run up to 4 concurrent worker goroutines
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for _, chunk := range batches {
		wg.Add(1)
		go func(ids []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			chunkGames := a.hydrateBatch(ids)
			if len(chunkGames) > 0 {
				mu.Lock()
				games = append(games, chunkGames...)
				mu.Unlock()
			}
		}(chunk)
	}

	wg.Wait()
	return games
}

func (a *Adapter) hydrateBatch(chunk []string) []ports.AccountGame {
	reqURL := fmt.Sprintf("%s?bigIds=%s&market=US&languages=en-us", DisplayCatalogURL, url.QueryEscape(strings.Join(chunk, ",")))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var catalogResp struct {
		Products []struct {
			ProductID           string `json:"ProductId"`
			ProductFamily       string `json:"ProductFamily"`
			LocalizedProperties []struct {
				ProductTitle string `json:"ProductTitle"`
			} `json:"LocalizedProperties"`
			Properties struct {
				ContentID               string   `json:"ContentId"`
				WuCategoryID            string   `json:"WuCategoryId"`
				PackageFamilyName       string   `json:"PackageFamilyName"`
				XboxConsoleGenOptimized []string `json:"XboxConsoleGenOptimized"`
				Attributes              []struct {
					Name                string   `json:"Name"`
					ApplicablePlatforms []string `json:"ApplicablePlatforms"`
				} `json:"Attributes"`
			} `json:"Properties"`
			DisplaySkuAvailabilities []struct {
				Sku struct {
					Properties struct {
						FulfillmentData struct {
							WuCategoryId string `json:"WuCategoryId"`
						} `json:"FulfillmentData"`
						Packages []struct {
							ContentID string   `json:"ContentId"`
							Platforms []string `json:"Platforms"`
						} `json:"Packages"`
					} `json:"Properties"`
				} `json:"Sku"`
			} `json:"DisplaySkuAvailabilities"`
		} `json:"Products"`
	}

	if err := json.Unmarshal(bodyBytes, &catalogResp); err != nil {
		return nil
	}

	var games []ports.AccountGame

	for _, prod := range catalogResp.Products {
		prodID := strings.TrimSpace(prod.ProductID)
		if prodID == "" {
			continue
		}

		var title string
		if len(prod.LocalizedProperties) > 0 {
			title = strings.TrimSpace(prod.LocalizedProperties[0].ProductTitle)
		}
		if title == "" {
			title = fmt.Sprintf("Xbox Game %s", prodID)
		}

		contentID := prod.Properties.ContentID
		wuCatID := prod.Properties.WuCategoryID

		// Discover platforms
		var platforms []string
		hasWin := false
		hasXbox := false

		// Check console generation attributes
		if len(prod.Properties.XboxConsoleGenOptimized) > 0 {
			hasXbox = true
		}

		// Check attributes applicable platforms
		for _, attr := range prod.Properties.Attributes {
			for _, ap := range attr.ApplicablePlatforms {
				apLower := strings.ToLower(ap)
				if strings.Contains(apLower, "desktop") || strings.Contains(apLower, "windows") || strings.Contains(apLower, "pc") {
					hasWin = true
				}
				if strings.Contains(apLower, "xbox") || strings.Contains(apLower, "console") {
					hasXbox = true
				}
			}
		}

		for _, dsa := range prod.DisplaySkuAvailabilities {
			if wuCatID == "" && dsa.Sku.Properties.FulfillmentData.WuCategoryId != "" {
				wuCatID = dsa.Sku.Properties.FulfillmentData.WuCategoryId
			}
			for _, pkg := range dsa.Sku.Properties.Packages {
				if contentID == "" && pkg.ContentID != "" {
					contentID = pkg.ContentID
				}
				for _, plat := range pkg.Platforms {
					platLower := strings.ToLower(plat)
					if strings.Contains(platLower, "windows") || strings.Contains(platLower, "pc") {
						hasWin = true
					}
					if strings.Contains(platLower, "xbox") || strings.Contains(platLower, "console") {
						hasXbox = true
					}
				}
			}
		}

		if hasWin {
			platforms = append(platforms, "Windows")
		}
		if hasXbox {
			platforms = append(platforms, "Xbox Series X|S", "Xbox One")
		}
		if len(platforms) == 0 {
			platforms = []string{"Windows", "Xbox Series X|S"}
		}

		appID := HashProductID(prodID)

		a.pkgMu.Lock()
		a.prodToAppID[prodID] = appID
		a.appToProdID[appID] = prodID
		pkgEntry, exists := a.packageMap[appID]
		if !exists {
			pkgEntry = &CachedPackage{
				ProductID:    prodID,
				ContentID:    contentID,
				WuCategoryID: wuCatID,
				Title:        title,
				Platforms:    platforms,
				ResolvedAt:   time.Now(),
			}
			a.packageMap[appID] = pkgEntry
		} else {
			if contentID != "" && pkgEntry.ContentID == "" {
				pkgEntry.ContentID = contentID
			}
			if wuCatID != "" && pkgEntry.WuCategoryID == "" {
				pkgEntry.WuCategoryID = wuCatID
			}
			pkgEntry.Title = title
			pkgEntry.Platforms = platforms
		}
		a.pkgMu.Unlock()

		// Cache in SQLite
		if a.db != nil {
			platStr := strings.Join(platforms, ", ")
			_ = a.db.UpsertGameMeta(prodID, "xbox", appID, title, platStr)
			_ = a.db.UpsertDepotMeta(db.DepotMeta{
				DepotID:   appID,
				AppID:     appID,
				Service:   "xbox",
				GameTitle: title,
				Platform:  platStr,
			})
		}

		games = append(games, ports.AccountGame{
			AppID:     appID,
			Service:   "xbox",
			Name:      title,
			Platforms: platforms,
		})
	}

	return games
}

// ResolveAppPlatforms returns known platforms for a given Xbox app ID
func (a *Adapter) ResolveAppPlatforms(appID int64, defaultName string) (string, []string) {
	a.pkgMu.RLock()
	pkg, ok := a.packageMap[appID]
	a.pkgMu.RUnlock()

	if ok && len(pkg.Platforms) > 0 {
		return pkg.Title, pkg.Platforms
	}

	// Check SQLite
	if a.db != nil {
		var name, platStr string
		err := a.db.QueryRow("SELECT name, platform FROM game_meta_cache WHERE app_id = ? AND service = 'xbox' LIMIT 1", appID).Scan(&name, &platStr)
		if err == nil && platStr != "" {
			parts := strings.Split(platStr, ",")
			var cleaned []string
			for _, p := range parts {
				if t := strings.TrimSpace(p); t != "" {
					cleaned = append(cleaned, t)
				}
			}
			return name, cleaned
		}
	}

	return defaultName, []string{"Windows", "Xbox Series X|S"}
}
