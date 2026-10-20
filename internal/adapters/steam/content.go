package steam

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"artesian/internal/manifest"
	"artesian/internal/ports"
)

var (
	steamChunkRegex = regexp.MustCompile(`^/depot/(\d+)/chunk/([a-fA-F0-9]{40})$`)
)

// GetCDNHosts returns known high-throughput Steam CDN hostnames
func (a *Adapter) GetCDNHosts() []string {
	return []string{
		"cache9-lhr1.steamcontent.com",
		"cache10-lhr1.steamcontent.com",
		"cache11-lhr1.steamcontent.com",
		"alibaba.cdn.steampipe.steamcontent.com",
		"steampipe.akamaized.net",
		"lancache.steamcontent.com",
	}
}

// BuildChunkURL formats a full HTTP download URL for a Steam depot chunk
func (a *Adapter) BuildChunkURL(host string, depotID int64, chunkHash string) string {
	return fmt.Sprintf("http://%s/depot/%d/chunk/%s", host, depotID, chunkHash)
}

// ParseChunkKey extracts depotID and chunkHash if URL is a Steam chunk
func (a *Adapter) ParseChunkKey(cacheKey string) (int64, string, bool) {
	matches := steamChunkRegex.FindStringSubmatch(cacheKey)
	if len(matches) == 3 {
		depotID, _ := strconv.ParseInt(matches[1], 10, 64)
		chunkHash := strings.ToLower(matches[2])
		return depotID, chunkHash, true
	}
	return 0, "", false
}

// DepotLookupURL returns the official SteamDB depot link
func (a *Adapter) DepotLookupURL(depotID int64) string {
	return fmt.Sprintf("https://steamdb.info/depot/%d", depotID)
}

// ResolveManifestChunks discovers and resolves chunks for a depot manifest via DB, disk, or Steam CM
func (a *Adapter) ResolveManifestChunks(ctx context.Context, appID, depotID, manifestID int64) ([]ports.ChunkMeta, error) {
	if depotID <= 0 {
		return nil, fmt.Errorf("invalid depot ID: %d", depotID)
	}

	// 1. Check SQLite indexed manifest cache
	if a.db != nil && manifestID > 0 {
		records, err := a.db.GetManifestChunks(depotID)
		if err == nil && len(records) > 0 {
			var metas []ports.ChunkMeta
			for _, r := range records {
				if r.ManifestID == manifestID || r.ManifestID == 0 {
					metas = append(metas, ports.ChunkMeta{
						ChunkID:   r.ChunkID,
						SizeBytes: r.SizeBytes,
					})
				}
			}
			if len(metas) > 0 {
				return metas, nil
			}
		}
	}

	// 2. Check local disk manifest pool
	if a.storage != nil {
		storageDir := a.storage.GetBaseDir()
		manifestDir := a.storage.GetManifestDir()
		depotCacheDir := a.storage.GetDepotCacheDir()

		records, _, err := manifest.FindAndParseDepotManifest(storageDir, manifestDir, depotCacheDir, depotID, manifestID)
		if err == nil && len(records) > 0 {
			if a.db != nil && manifestID > 0 {
				_ = a.db.SaveManifestChunks(depotID, manifestID, records)
			}
			var metas []ports.ChunkMeta
			for _, r := range records {
				metas = append(metas, ports.ChunkMeta{
					ChunkID:   r.ChunkID,
					SizeBytes: r.SizeBytes,
				})
			}
			return metas, nil
		}
	}

	// 3. Resolve genuine manifest via Steam CM in pure Go
	if manifestID <= 0 {
		return nil, fmt.Errorf("no manifest GID discovered for depot %d", depotID)
	}

	if a.db == nil {
		return nil, fmt.Errorf("database unavailable for Steam session")
	}

	sess, err := a.db.GetAuthSession(a.ID())
	if err != nil || sess == nil || !sess.IsActive || sess.RefreshToken == "" {
		return nil, fmt.Errorf("no active Steam session found in database for depot %d", depotID)
	}

	cleanSteamID := strings.Trim(sess.AccountID, "'\"")
	steamID64, _ := strconv.ParseUint(cleanSteamID, 10, 64)
	if steamID64 == 0 {
		return nil, fmt.Errorf("invalid SteamID64 for active session")
	}

	log.Printf("[Steam Content] Authenticating with Steam CM to resolve manifest for depot %d (manifest %d)...", depotID, manifestID)
	cmClient, err := NewCMClient(ctx, sess.Username, sess.RefreshToken, steamID64)
	if err != nil {
		return nil, fmt.Errorf("Steam CM connection failed: %w", err)
	}
	defer cmClient.Close()

	reqCode, err := cmClient.GetManifestRequestCode(ctx, uint32(appID), uint32(depotID), uint64(manifestID), "public")
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest request code: %w", err)
	}

	cdnHosts := a.GetCDNHosts()
	manifestPath := fmt.Sprintf("/depot/%d/manifest/%d/5/%d", depotID, manifestID, reqCode)
	var rawData []byte

	for _, host := range cdnHosts {
		reqURL := fmt.Sprintf("http://%s%s", host, manifestPath)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Valve/Steam HTTP Client 1.0")
		req.Header.Set("X-Artesian-Prefill", "1")
		req.Header.Set("X-Artesian-Loop", "1")

		client := a.cdnHTTPClient
		if client == nil {
			client = a.httpClient
		}

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && len(data) > 0 {
				rawData = data
				break
			}
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
	}

	if len(rawData) == 0 {
		return nil, fmt.Errorf("failed to download manifest for depot %d from Steam CDN", depotID)
	}

	// Save raw manifest to disk pool
	if a.storage != nil {
		manifestDir := a.storage.GetManifestDir()
		_ = os.MkdirAll(manifestDir, 0755)
		savePath := filepath.Join(manifestDir, fmt.Sprintf("%d_%d.manifest", depotID, manifestID))
		_ = os.WriteFile(savePath, rawData, 0644)
	}

	// Parse genuine manifest payload into chunks
	parsedRecords, err := manifest.ParseManifestBytes(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest bytes: %w", err)
	}

	if a.db != nil && len(parsedRecords) > 0 {
		_ = a.db.SaveManifestChunks(depotID, manifestID, parsedRecords)
	}

	var metas []ports.ChunkMeta
	for _, r := range parsedRecords {
		metas = append(metas, ports.ChunkMeta{
			ChunkID:   r.ChunkID,
			SizeBytes: r.SizeBytes,
		})
	}
	return metas, nil
}
