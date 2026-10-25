package xbox

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"artesian/internal/db"
)

const (
	PackagesPCBaseURL = "https://packagespc.xboxlive.com/GetBasePackage"
)

// ResolvePackage resolves the download CDN roots and relative paths to .msixvc, .phf, and .ecd
func (a *Adapter) ResolvePackage(appID int64) (*CachedPackage, error) {
	if err := a.ensureValidTokens(); err != nil {
		return nil, err
	}

	a.pkgMu.RLock()
	pkg, exists := a.packageMap[appID]
	a.pkgMu.RUnlock()

	if !exists || pkg == nil || pkg.ContentID == "" {
		// Attempt to resolve contentId from Display Catalog or SQLite
		if err := a.FetchAppInfo(appID); err != nil {
			return nil, fmt.Errorf("could not retrieve package metadata for app %d: %w", appID, err)
		}
		a.pkgMu.RLock()
		pkg = a.packageMap[appID]
		a.pkgMu.RUnlock()
	}

	if pkg == nil || pkg.ContentID == "" {
		return nil, fmt.Errorf("no ContentID found for app %d", appID)
	}

	// If already fully resolved with sizes, return cached copy
	if pkg.BasePackageURL != "" && pkg.PackageSize > 0 && len(pkg.CdnRoots) > 0 {
		return pkg, nil
	}

	a.authMu.RLock()
	uhs := a.userHash
	token := a.updateXstsToken
	if token == "" {
		token = a.xblXstsToken
	}
	a.authMu.RUnlock()

	if uhs == "" || token == "" {
		return nil, fmt.Errorf("Xbox authentication required to resolve package URLs")
	}

	// Path A: Call Xbox Package Service
	reqURL := fmt.Sprintf("%s/%s", PackagesPCBaseURL, pkg.ContentID)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("XBL3.0 x=%s;%s", uhs, token))
	req.Header.Set("User-Agent", "Xbox/2401.1001.17.0")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("package service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			var pkgResp struct {
				PackageFound         bool     `json:"PackageFound"`
				PackageURL           string   `json:"PackageUrl"`
				BasePackageURL       string   `json:"BasePackageUrl"`
				PieceHashFileURL     string   `json:"PieceHashFileUrl"`
				ContentDescriptorURL string   `json:"ContentDescriptorUrl"`
				CdnRoots             []string `json:"CdnRoots"`
				CdnRoot              string   `json:"CdnRoot"`
				PackageSize          int64    `json:"PackageSize"`
				SizeInBytes          int64    `json:"SizeInBytes"`
				PackageFiles         []struct {
					FileName               string   `json:"FileName"`
					FileSize               int64    `json:"FileSize"`
					RelativeURL            string   `json:"RelativeUrl"`
					CdnRootPaths           []string `json:"CdnRootPaths"`
					BackgroundCdnRootPaths []string `json:"BackgroundCdnRootPaths"`
				} `json:"PackageFiles"`
				Packages []struct {
					PackageURL           string   `json:"PackageUrl"`
					BasePackageURL       string   `json:"BasePackageUrl"`
					PieceHashFileURL     string   `json:"PieceHashFileUrl"`
					ContentDescriptorURL string   `json:"ContentDescriptorUrl"`
					CdnRoots             []string `json:"CdnRoots"`
					PackageSize          int64    `json:"PackageSize"`
				} `json:"Packages"`
			}
			if err := json.Unmarshal(bodyBytes, &pkgResp); err == nil {
				basePkg := pkgResp.BasePackageURL
				if basePkg == "" {
					basePkg = pkgResp.PackageURL
				}
				phf := pkgResp.PieceHashFileURL
				ecd := pkgResp.ContentDescriptorURL
				cdnRoots := pkgResp.CdnRoots
				if len(cdnRoots) == 0 && pkgResp.CdnRoot != "" {
					cdnRoots = []string{pkgResp.CdnRoot}
				}
				pkgSize := pkgResp.PackageSize
				if pkgSize <= 0 {
					pkgSize = pkgResp.SizeInBytes
				}

				// 1. Inspect PackageFiles (standard response from packagespc.xboxlive.com)
				if len(pkgResp.PackageFiles) > 0 {
					var bestIdx = -1
					var maxArchiveSize int64 = -1

					for idx, f := range pkgResp.PackageFiles {
						nameLower := strings.ToLower(f.FileName)
						isArchive := strings.HasSuffix(nameLower, ".xvc") ||
							strings.HasSuffix(nameLower, ".msixvc") ||
							strings.HasSuffix(nameLower, ".appx") ||
							strings.HasSuffix(nameLower, ".msix")
						if isArchive && f.FileSize > maxArchiveSize {
							bestIdx = idx
							maxArchiveSize = f.FileSize
						}
					}
					if bestIdx == -1 {
						for idx, f := range pkgResp.PackageFiles {
							if f.FileSize > maxArchiveSize {
								bestIdx = idx
								maxArchiveSize = f.FileSize
							}
						}
					}

					if bestIdx >= 0 {
						best := pkgResp.PackageFiles[bestIdx]
						pkgSize = best.FileSize

						var roots []string
						roots = append(roots, best.CdnRootPaths...)
						if len(roots) == 0 {
							roots = append(roots, best.BackgroundCdnRootPaths...)
						}

						pathPrefix := ""
						for _, r := range roots {
							rTrim := strings.TrimSpace(r)
							if rTrim == "" {
								continue
							}
							if strings.HasPrefix(rTrim, "http://") || strings.HasPrefix(rTrim, "https://") {
								parsed, err := url.Parse(rTrim)
								if err == nil {
									if parsed.Host != "" {
										cdnRoots = append(cdnRoots, parsed.Host)
									}
									if pathPrefix == "" && parsed.Path != "" {
										pathPrefix = parsed.Path
									}
								}
							} else {
								cdnRoots = append(cdnRoots, rTrim)
							}
						}

						rel := strings.TrimPrefix(best.RelativeURL, "/")
						if pathPrefix != "" {
							basePkg = "/" + strings.Trim(pathPrefix, "/") + "/" + rel
						} else {
							basePkg = "/" + rel
						}
					}
				}

				if basePkg == "" && len(pkgResp.Packages) > 0 {
					first := pkgResp.Packages[0]
					basePkg = first.BasePackageURL
					if basePkg == "" {
						basePkg = first.PackageURL
					}
					if phf == "" {
						phf = first.PieceHashFileURL
					}
					if ecd == "" {
						ecd = first.ContentDescriptorURL
					}
					if len(cdnRoots) == 0 {
						cdnRoots = first.CdnRoots
					}
					if pkgSize <= 0 {
						pkgSize = first.PackageSize
					}
				}

				if len(cdnRoots) == 0 {
					cdnRoots = []string{"assets1.xboxlive.com", "xvcf1.xboxlive.com"}
				}

				// Clean paths (strip leading hosts if already fully qualified)
				basePkg = cleanCDNPath(basePkg)
				phf = cleanCDNPath(phf)
				ecd = cleanCDNPath(ecd)

				// Synthesize .phf and .ecd relative paths if omitted by service
				if phf == "" && basePkg != "" {
					phf = replaceExtension(basePkg, ".phf")
				}
				if ecd == "" && basePkg != "" {
					ecd = replaceExtension(basePkg, ".ecd")
				}

				// If package size is not known, probe CDN directly
				if pkgSize <= 0 && len(cdnRoots) > 0 && basePkg != "" {
					pkgSize = a.probeRemoteFileSize(cdnRoots[0], basePkg)
				}
				phfSize := a.probeRemoteFileSize(cdnRoots[0], phf)
				if phfSize <= 0 {
					phf = ""
					phfSize = 0
				}
				ecdSize := a.probeRemoteFileSize(cdnRoots[0], ecd)
				if ecdSize <= 0 {
					ecd = ""
					ecdSize = 0
				}

				a.pkgMu.Lock()
				pkg.BasePackageURL = basePkg
				pkg.PieceHashFileURL = phf
				pkg.ContentDescriptorURL = ecd
				pkg.CdnRoots = cdnRoots
				pkg.PackageSize = pkgSize
				pkg.PhfSize = phfSize
				pkg.EcdSize = ecdSize
				pkg.ResolvedAt = time.Now()
				a.pkgMu.Unlock()

				// Update SQLite depot metadata
				if a.db != nil {
					_ = a.db.UpsertDepotMeta(db.DepotMeta{
						DepotID:    appID,
						AppID:      appID,
						Service:    "xbox",
						GameTitle:  pkg.Title,
						Platform:   strings.Join(pkg.Platforms, ", "),
						TotalSize:  pkgSize,
						ManifestID: 1,
					})
				}

				log.Printf("[Xbox Package Service] Resolved package for %s: %s (Size: %d bytes, CDN: %s)",
					pkg.Title, basePkg, pkgSize, cdnRoots[0])
				return pkg, nil
			}
		}
	}

	// Path B: Delivery Optimization / Store Fallback if Package Service did not resolve
	log.Printf("[Xbox Package Service] PackagesPC returned %d for content %s. Initializing fallback delivery endpoint...", resp.StatusCode, pkg.ContentID)
	cdnRoots := []string{"http://assets1.xboxlive.com", "http://xvcf1.xboxlive.com", "http://dl.delivery.mp.microsoft.com"}
	defaultBasePkg := fmt.Sprintf("/filestream/%s.msixvc", pkg.ContentID)
	defaultPhf := fmt.Sprintf("/filestream/%s.phf", pkg.ContentID)
	defaultEcd := fmt.Sprintf("/filestream/%s.ecd", pkg.ContentID)

	pkgSize := a.probeRemoteFileSize(cdnRoots[0], defaultBasePkg)
	if pkgSize <= 0 {
		pkgSize = 10 * 1024 * 1024 * 1024 // 10 GB baseline
	}

	a.pkgMu.Lock()
	pkg.BasePackageURL = defaultBasePkg
	pkg.PieceHashFileURL = defaultPhf
	pkg.ContentDescriptorURL = defaultEcd
	pkg.CdnRoots = cdnRoots
	pkg.PackageSize = pkgSize
	pkg.PhfSize = 512 * 1024
	pkg.EcdSize = 4 * 1024
	pkg.ResolvedAt = time.Now()
	a.pkgMu.Unlock()

	if a.db != nil {
		_ = a.db.UpsertDepotMeta(db.DepotMeta{
			DepotID:    appID,
			AppID:      appID,
			Service:    "xbox",
			GameTitle:  pkg.Title,
			Platform:   strings.Join(pkg.Platforms, ", "),
			TotalSize:  pkgSize,
			ManifestID: 1,
		})
	}

	return pkg, nil
}

func cleanCDNPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.Index(raw, "?"); idx != -1 {
		raw = raw[:idx]
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parts := strings.SplitN(raw, "/", 4)
		if len(parts) >= 4 {
			return "/" + parts[3]
		}
	}
	if !strings.HasPrefix(raw, "/") {
		return "/" + raw
	}
	return raw
}

func replaceExtension(path, newExt string) string {
	idx := strings.LastIndex(path, ".")
	if idx != -1 {
		return path[:idx] + newExt
	}
	return path + newExt
}

// probeRemoteFileSize queries Content-Length or Content-Range to determine exact file size
func (a *Adapter) probeRemoteFileSize(cdnRoot, relPath string) int64 {
	if cdnRoot == "" || relPath == "" {
		return 0
	}
	cdnRoot = strings.TrimSuffix(cdnRoot, "/")
	if !strings.HasPrefix(cdnRoot, "http://") && !strings.HasPrefix(cdnRoot, "https://") {
		cdnRoot = "http://" + cdnRoot
	}
	targetURL := fmt.Sprintf("%s%s", cdnRoot, relPath)

	// 1. Try HEAD request
	headReq, err := http.NewRequest("HEAD", targetURL, nil)
	if err == nil {
		headReq.Header.Set("User-Agent", "Xbox/2401.1001.17.0")
		headReq.Header.Set("X-Artesian-Prefill", "1")
		resp, err := a.cdnHTTPClient.Do(headReq)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && resp.ContentLength > 0 {
				return resp.ContentLength
			}
		}
	}

	// 2. Try GET with Range: bytes=0-0 to inspect Content-Range: bytes 0-0/TOTAL
	rangeReq, err := http.NewRequest("GET", targetURL, nil)
	if err == nil {
		rangeReq.Header.Set("User-Agent", "Xbox/2401.1001.17.0")
		rangeReq.Header.Set("Range", "bytes=0-0")
		rangeReq.Header.Set("X-Artesian-Prefill", "1")
		resp, err := a.cdnHTTPClient.Do(rangeReq)
		if err == nil {
			defer resp.Body.Close()
			if cr := resp.Header.Get("Content-Range"); cr != "" {
				// Format: "bytes 0-0/52428800000"
				slashIdx := strings.LastIndex(cr, "/")
				if slashIdx != -1 && slashIdx+1 < len(cr) {
					totalStr := strings.TrimSpace(cr[slashIdx+1:])
					if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil && total > 0 {
						return total
					}
				}
			}
		}
	}

	return 0
}
