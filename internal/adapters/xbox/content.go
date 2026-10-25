package xbox

import (
	"context"
	"fmt"
	"strings"

	"artesian/internal/ports"
)

const (
	XboxSliceBlockSize = int64(1048576) // 1MB block size standard for Delivery Optimization and Xbox PC client
)

// GetCDNHosts returns active public CDN hostnames for Xbox game archives
func (a *Adapter) GetCDNHosts() []string {
	return []string{
		"assets1.xboxlive.com",
		"xvcf1.xboxlive.com",
		"assets2.xboxlive.com",
		"xvcf2.xboxlive.com",
		"dl.delivery.mp.microsoft.com",
	}
}

// BuildChunkURL constructs the direct CDN request URL for a chunk or file slice
func (a *Adapter) BuildChunkURL(host string, depotID int64, chunkHash string) string {
	rawPath := chunkHash
	// Strip range suffix if present: e.g. "/path.msixvc@range=0-1048575" -> "/path.msixvc"
	if idx := strings.Index(rawPath, "@range="); idx != -1 {
		rawPath = rawPath[:idx]
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	return fmt.Sprintf("http://%s%s", host, rawPath)
}

// ParseChunkKey extracts asset/depot ID and normalized key for caching
func (a *Adapter) ParseChunkKey(cacheKey string) (int64, string, bool) {
	cleaned := cacheKey
	if idx := strings.Index(cleaned, "?"); idx != -1 {
		cleaned = cleaned[:idx] // strip query parameters
	}

	a.pkgMu.RLock()
	defer a.pkgMu.RUnlock()

	// 1. Check if matches any active cached package
	for appID, pkg := range a.packageMap {
		baseClean := strings.TrimPrefix(pkg.BasePackageURL, "/")
		keyClean := strings.TrimPrefix(cleaned, "/")
		if strings.HasPrefix(keyClean, baseClean) ||
			(pkg.ContentID != "" && strings.Contains(keyClean, pkg.ContentID)) {
			return appID, cleaned, true
		}
	}

	// 2. Generic match for Xbox CDN paths
	if strings.HasSuffix(cleaned, ".msixvc") ||
		strings.HasSuffix(cleaned, ".xvc") ||
		strings.HasSuffix(cleaned, ".phf") ||
		strings.HasSuffix(cleaned, ".ecd") ||
		strings.Contains(cleaned, "/filestream/") {
		return HashProductID(cleaned), cleaned, true
	}

	return 0, "", false
}

// DepotLookupURL returns external inspection link for Xbox packages
func (a *Adapter) DepotLookupURL(depotID int64) string {
	a.pkgMu.RLock()
	prodID, ok := a.appToProdID[depotID]
	a.pkgMu.RUnlock()

	if ok && prodID != "" {
		return fmt.Sprintf("https://www.xbox.com/games/store/%s", strings.ToLower(prodID))
	}
	return "https://www.xbox.com/games/store"
}

// ResolveManifestChunks resolves package archives, .phf, .ecd, and 1MB slices for prefilling
func (a *Adapter) ResolveManifestChunks(ctx context.Context, appID, depotID, manifestID int64) ([]ports.ChunkMeta, error) {
	pkg, err := a.ResolvePackage(appID)
	if err != nil {
		return nil, fmt.Errorf("package resolution failed for app %d: %w", appID, err)
	}

	var chunks []ports.ChunkMeta

	// 1. Add Content Descriptor (.ecd) file chunk
	if pkg.ContentDescriptorURL != "" {
		chunks = append(chunks, ports.ChunkMeta{
			ChunkID:   pkg.ContentDescriptorURL,
			SizeBytes: pkg.EcdSize,
		})
	}

	// 2. Add Piece Hash File (.phf) chunk
	if pkg.PieceHashFileURL != "" {
		chunks = append(chunks, ports.ChunkMeta{
			ChunkID:   pkg.PieceHashFileURL,
			SizeBytes: pkg.PhfSize,
		})
	}

	// 3. Add monolithic archive slices (.msixvc or .xvc) in 1MB blocks
	if pkg.BasePackageURL != "" && pkg.PackageSize > 0 {
		totalSize := pkg.PackageSize
		blockSize := XboxSliceBlockSize

		for start := int64(0); start < totalSize; start += blockSize {
			select {
			case <-ctx.Done():
				return chunks, ctx.Err()
			default:
			}

			end := start + blockSize - 1
			if end >= totalSize {
				end = totalSize - 1
			}

			chunkID := fmt.Sprintf("%s@range=%d-%d", pkg.BasePackageURL, start, end)
			chunks = append(chunks, ports.ChunkMeta{
				ChunkID:   chunkID,
				SizeBytes: end - start + 1,
			})
		}
	}

	return chunks, nil
}
