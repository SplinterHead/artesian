package epic

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"artesian/internal/db"
	"artesian/internal/ports"
)

type v2AssetResponse struct {
	Elements []struct {
		AppName       string `json:"appName"`
		CatalogItemID string `json:"catalogItemId"`
		Namespace     string `json:"namespace"`
		BuildVersion  string `json:"buildVersion"`
		Manifests     []struct {
			URI         string `json:"uri"`
			QueryParams []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"queryParams"`
		} `json:"manifests"`
	} `json:"elements"`
}

// ResolveBaseAppID returns the base App ID (for Epic, appID and depotID are identical)
func (a *Adapter) ResolveBaseAppID(depotID int64) int64 {
	return depotID
}

// ResolveDepot returns game title, platform, and sizing metadata for an Epic asset
func (a *Adapter) ResolveDepot(depotID int64, userAgent string) ports.DepotInfo {
	if depotID <= 0 {
		return ports.DepotInfo{
			DepotID:   0,
			AppID:     0,
			GameTitle: "Epic Games Launcher",
			Platform:  "Windows",
		}
	}

	a.gamesMu.RLock()
	info, ok := a.gamesCache[depotID]
	a.gamesMu.RUnlock()

	if ok && info != nil {
		platform := "Windows"
		if len(info.Platforms) > 0 {
			platform = info.Platforms[0]
		}
		return ports.DepotInfo{
			DepotID:    depotID,
			AppID:      depotID,
			GameTitle:  info.Title,
			Platform:   platform,
			TotalSize:  info.TotalSize,
			ManifestID: 1,
		}
	}

	// Check SQLite depot_meta_cache
	if a.db != nil {
		meta, err := a.db.GetDepotMeta(depotID)
		if err == nil && meta != nil && meta.GameTitle != "" {
			return ports.DepotInfo{
				DepotID:    meta.DepotID,
				AppID:      meta.AppID,
				GameTitle:  meta.GameTitle,
				Platform:   meta.Platform,
				TotalSize:  meta.TotalSize,
				ManifestID: meta.ManifestID,
			}
		}
	}

	return ports.DepotInfo{
		DepotID:   depotID,
		AppID:     depotID,
		GameTitle: fmt.Sprintf("Epic Game %d", depotID),
		Platform:  "Windows",
	}
}

// FetchAppInfo requests the v2 asset manifest for an Epic game, downloads and parses chunks
func (a *Adapter) FetchAppInfo(appID int64) error {
	token := a.GetAccessToken()
	if token == "" {
		return fmt.Errorf("epic games account not authenticated")
	}

	a.gamesMu.RLock()
	info := a.gamesCache[appID]
	a.gamesMu.RUnlock()

	// If info is not yet in memory, discover library first
	if info == nil {
		_, err := a.GetOwnedGames()
		if err != nil {
			return fmt.Errorf("failed to load epic owned games: %w", err)
		}
		a.gamesMu.RLock()
		info = a.gamesCache[appID]
		a.gamesMu.RUnlock()
	}

	if info == nil {
		return fmt.Errorf("epic game info not found for app ID %d", appID)
	}

	// Query v2 assets endpoint for distribution points and .manifest location
	v2URL := fmt.Sprintf(
		"https://launcher-public-service-prod06.ol.epicgames.com/launcher/api/public/assets/v2/platform/Windows/namespace/%s/catalogItem/%s/app/%s/label/Live",
		url.PathEscape(info.Namespace),
		url.PathEscape(info.CatalogItemID),
		url.PathEscape(info.AppName),
	)

	req, err := http.NewRequest("GET", v2URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query v2 assets for %s: %w", info.Title, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("epic v2 assets returned HTTP %d for %s", resp.StatusCode, info.Title)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var v2Resp v2AssetResponse
	if err := json.Unmarshal(bodyBytes, &v2Resp); err != nil {
		return fmt.Errorf("failed to decode v2 assets: %w", err)
	}

	if len(v2Resp.Elements) == 0 || len(v2Resp.Elements[0].Manifests) == 0 {
		return fmt.Errorf("no manifests returned for epic app %s", info.Title)
	}

	manifestEntry := v2Resp.Elements[0].Manifests[0]
	manifestURI := manifestEntry.URI
	if manifestURI == "" {
		return fmt.Errorf("empty manifest URI for epic app %s", info.Title)
	}

	// Append query parameters if present (signed CDN tokens)
	manifestDownloadURL := manifestURI
	if len(manifestEntry.QueryParams) > 0 {
		vals := url.Values{}
		for _, q := range manifestEntry.QueryParams {
			vals.Add(q.Name, q.Value)
		}
		if strings.Contains(manifestDownloadURL, "?") {
			manifestDownloadURL += "&" + vals.Encode()
		} else {
			manifestDownloadURL += "?" + vals.Encode()
		}
	}

	log.Printf("[Epic Metadata] Downloading manifest for %s from %s...", info.Title, manifestURI)

	// Download the .manifest file via cdnHTTPClient (direct DNS)
	mReq, mErr := http.NewRequest("GET", manifestDownloadURL, nil)
	if mErr != nil {
		return mErr
	}
	mReq.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")
	mReq.Header.Set("X-Artesian-Prefill", "1")
	mReq.Header.Set("X-Artesian-Loop", "1")

	mResp, mErr := a.cdnHTTPClient.Do(mReq)
	if mErr != nil || mResp.StatusCode != http.StatusOK {
		if mResp != nil {
			_ = mResp.Body.Close()
		}
		// Fallback to base HTTP client
		mResp, mErr = a.httpClient.Do(mReq)
		if mErr != nil || mResp.StatusCode != http.StatusOK {
			if mResp != nil {
				_ = mResp.Body.Close()
			}
			return fmt.Errorf("failed to download manifest file: %v", mErr)
		}
	}
	defer mResp.Body.Close()

	manifestBytes, err := io.ReadAll(mResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read manifest payload: %w", err)
	}

	// Parse manifest binary/JSON payload
	parsed, err := ParseManifest(manifestBytes)
	if err != nil {
		return fmt.Errorf("failed to parse epic manifest for %s: %w", info.Title, err)
	}

	baseDir := GetBaseDir(manifestURI)
	info.BaseDir = baseDir
	info.ManifestURI = manifestURI
	info.TotalSize = parsed.TotalSize

	a.manifestsMu.Lock()
	a.manifests[appID] = parsed
	a.manifestsMu.Unlock()

	// Map every chunk to its full download URL
	a.chunkURLsMu.Lock()
	for _, c := range parsed.Chunks {
		chunkURL := fmt.Sprintf("%s/%s", strings.TrimRight(baseDir, "/"), c.RelativePath)
		a.chunkURLs[c.ChunkID] = chunkURL
	}
	a.chunkURLsMu.Unlock()

	// Persist raw manifest to disk pool
	if a.storage != nil {
		manifestDir := a.storage.GetManifestDir()
		_ = os.MkdirAll(manifestDir, 0755)
		savePath := filepath.Join(manifestDir, fmt.Sprintf("epic_%d_1.manifest", appID))
		_ = os.WriteFile(savePath, manifestBytes, 0644)
	}

	// Persist to SQLite depot_meta_cache and depot_manifest_chunks
	if a.db != nil {
		meta := db.DepotMeta{
			DepotID:     appID,
			AppID:       appID,
			Service:     a.ID(),
			GameTitle:   info.Title,
			Platform:    "Windows",
			TotalSize:   parsed.TotalSize,
			ManifestID:  1,
			LastUpdated: time.Now(),
		}
		_ = a.db.UpsertDepotMeta(meta)

		var chunkRecords []db.ManifestChunkRecord
		for _, c := range parsed.Chunks {
			chunkRecords = append(chunkRecords, db.ManifestChunkRecord{
				DepotID:        appID,
				ManifestID:     1,
				ChunkID:        c.ChunkID,
				SizeBytes:      c.SizeBytes,
				OriginalSize:   c.SizeBytes,
				CompressedSize: c.SizeBytes,
			})
		}
		if len(chunkRecords) > 0 {
			_ = a.db.SaveManifestChunks(appID, 1, chunkRecords)
		}
	}

	log.Printf("[Epic Metadata] Successfully parsed and cached manifest for %s (%d chunks, %d bytes)",
		info.Title, len(parsed.Chunks), parsed.TotalSize)

	return nil
}

// CheckAppUpdate checks Epic Launcher assets for buildVersion updates
func (a *Adapter) CheckAppUpdate(appID int64, currentChangeNumber uint64) (*ports.AppUpdateCheck, error) {
	token := a.GetAccessToken()
	if token == "" {
		return nil, fmt.Errorf("epic games account not authenticated")
	}

	a.gamesMu.RLock()
	info := a.gamesCache[appID]
	a.gamesMu.RUnlock()

	assetsURL := "https://launcher-public-service-prod06.ol.epicgames.com/launcher/api/public/assets/Windows?label=Live"
	req, err := http.NewRequest("GET", assetsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "EpicGamesLauncher/14.0.0")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("epic assets check returned HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var assets []launcherAssetItem
	if err := json.Unmarshal(bodyBytes, &assets); err != nil {
		return nil, err
	}

	var targetAsset *launcherAssetItem
	for _, it := range assets {
		if HashAppName(it.AppName) == appID {
			targetAsset = &it
			break
		}
	}

	if targetAsset == nil {
		name := "Epic Game"
		if info != nil {
			name = info.Title
		}
		return &ports.AppUpdateCheck{
			AppID:           appID,
			Name:            name,
			OldChangeNumber: currentChangeNumber,
			NewChangeNumber: currentChangeNumber,
			Updated:         false,
		}, nil
	}

	name := targetAsset.AppName
	if info != nil && info.Title != "" {
		name = info.Title
	}

	newChangeNumber := uint64(HashAppName(targetAsset.BuildVersion))

	// Baseline initialization: if app has no recorded change number (0)
	if currentChangeNumber == 0 {
		_ = a.FetchAppInfo(appID)
		return &ports.AppUpdateCheck{
			AppID:           appID,
			Name:            name,
			OldChangeNumber: 0,
			NewChangeNumber: newChangeNumber,
			Updated:         false,
		}, nil
	}

	hasUpdate := newChangeNumber != currentChangeNumber
	if hasUpdate {
		_ = a.FetchAppInfo(appID)
		log.Printf("[Epic Update Check] UPDATE DETECTED for %s (App %d): Build changed to %s (Change #%d)",
			name, appID, targetAsset.BuildVersion, newChangeNumber)
	}

	return &ports.AppUpdateCheck{
		AppID:           appID,
		Name:            name,
		OldChangeNumber: currentChangeNumber,
		NewChangeNumber: newChangeNumber,
		Updated:         hasUpdate,
	}, nil
}
