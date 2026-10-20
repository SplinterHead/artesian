package epic

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"artesian/internal/ports"
)

var (
	epicChunkRegex = regexp.MustCompile(`([0-9a-fA-F]{16}_[0-9a-fA-F]{32})\.chunk`)
)

// GetCDNHosts returns known high-speed Epic Games CDN hosts
func (a *Adapter) GetCDNHosts() []string {
	return []string{
		"epicgames-download1.akamaized.net",
		"download.epicgames.com",
		"fastly-download.epicgames.com",
	}
}

// BuildChunkURL formats the full CDN URL for downloading an Epic chunk
func (a *Adapter) BuildChunkURL(host string, depotID int64, chunkHash string) string {
	a.chunkURLsMu.RLock()
	cachedURL, found := a.chunkURLs[chunkHash]
	a.chunkURLsMu.RUnlock()

	if found && cachedURL != "" {
		if u, err := url.Parse(cachedURL); err == nil {
			u.Host = host
			u.Scheme = "http"
			return u.String()
		}
		return cachedURL
	}

	// Fallback reconstruction if manifest cache is unpopulated
	return fmt.Sprintf("http://%s/%s.chunk", host, chunkHash)
}

// ParseChunkKey extracts chunkHash from an incoming URL path
func (a *Adapter) ParseChunkKey(cacheKey string) (int64, string, bool) {
	matches := epicChunkRegex.FindStringSubmatch(cacheKey)
	if len(matches) == 2 {
		return 0, strings.ToUpper(matches[1]), true
	}
	return 0, "", false
}

// DepotLookupURL returns the official Epic Games store URL
func (a *Adapter) DepotLookupURL(depotID int64) string {
	return "https://store.epicgames.com"
}

// ResolveManifestChunks resolves chunks for an Epic game from memory, SQLite, or the v2 asset API
func (a *Adapter) ResolveManifestChunks(ctx context.Context, appID, depotID, manifestID int64) ([]ports.ChunkMeta, error) {
	// 1. Check in-memory manifest cache
	a.manifestsMu.RLock()
	m, ok := a.manifests[appID]
	a.manifestsMu.RUnlock()

	if ok && m != nil && len(m.Chunks) > 0 {
		return m.ToChunkMeta(), nil
	}

	// 2. Check SQLite depot_manifest_chunks
	if a.db != nil {
		records, err := a.db.GetManifestChunks(appID)
		if err == nil && len(records) > 0 {
			metas := make([]ports.ChunkMeta, len(records))
			for i, r := range records {
				metas[i] = ports.ChunkMeta{
					ChunkID:   r.ChunkID,
					SizeBytes: r.SizeBytes,
				}
			}
			return metas, nil
		}
	}

	// 3. Fetch fresh manifest from Epic Launcher API
	if err := a.FetchAppInfo(appID); err != nil {
		return nil, fmt.Errorf("failed to fetch epic manifest for app %d: %w", appID, err)
	}

	a.manifestsMu.RLock()
	m, ok = a.manifests[appID]
	a.manifestsMu.RUnlock()

	if ok && m != nil && len(m.Chunks) > 0 {
		return m.ToChunkMeta(), nil
	}

	return nil, fmt.Errorf("no chunks found in epic manifest for app %d", appID)
}
