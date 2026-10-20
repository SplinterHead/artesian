package steam

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"artesian/internal/db"
	"artesian/internal/ports"
)

// FormatPlatform resolves human-friendly platform labels like "Mac 64bit", "Windows", "Linux 64bit"
func FormatPlatform(oslist, osarch, userAgent string) string {
	arch := strings.TrimSpace(osarch)
	if arch != "" && !strings.HasSuffix(arch, "bit") {
		arch = arch + "bit"
	}

	uaLower := strings.ToLower(userAgent)
	isMacUA := strings.Contains(uaLower, "macintosh") || strings.Contains(uaLower, "mac os x") || strings.Contains(uaLower, "darwin")
	isWinUA := strings.Contains(uaLower, "windows")
	isLinuxUA := strings.Contains(uaLower, "linux")

	// 1. Client HTTP User-Agent is the ground truth of what machine is downloading
	if isMacUA {
		if arch != "" {
			return "Mac " + arch
		}
		return "Mac 64bit"
	} else if isLinuxUA {
		if arch != "" {
			return "Linux " + arch
		}
		return "Linux 64bit"
	} else if isWinUA {
		if arch != "" {
			return "Windows " + arch
		}
		if strings.Contains(uaLower, "win64") || strings.Contains(uaLower, "x64") || strings.Contains(uaLower, "wow64") {
			return "Windows 64bit"
		}
		return "Windows"
	}

	// 2. From Steam depot configuration if User-Agent is unspecified/generic
	osLower := strings.ToLower(strings.TrimSpace(oslist))
	if osLower != "" {
		if strings.Contains(osLower, "macos") || strings.Contains(osLower, "osx") || strings.Contains(osLower, "mac") {
			if arch != "" {
				return "Mac " + arch
			}
			return "Mac 64bit"
		}
		if strings.Contains(osLower, "windows") || strings.Contains(osLower, "win") {
			if arch != "" {
				return "Windows " + arch
			}
			return "Windows"
		}
		if strings.Contains(osLower, "linux") {
			if arch != "" {
				return "Linux " + arch
			}
			return "Linux 64bit"
		}
		if len(oslist) > 0 {
			return strings.ToUpper(oslist[:1]) + strings.ToLower(oslist[1:])
		}
		return oslist
	}

	return "PC"
}

// ResolveBaseAppID finds the base App ID from a depot ID
func (a *Adapter) ResolveBaseAppID(depotID int64) int64 {
	if depotID <= 0 {
		return 0
	}
	if depotID%10 == 0 {
		return depotID
	}
	return depotID - (depotID % 10)
}

// ResolveDepot returns fresh game title, platform, and total size for a depot with caching
func (a *Adapter) ResolveDepot(depotID int64, userAgent string) ports.DepotInfo {
	if depotID <= 0 {
		return ports.DepotInfo{GameTitle: "Steam Game Client", Platform: FormatPlatform("", "", userAgent)}
	}

	// 1. In-memory cache
	a.depotInfoCacheMu.RLock()
	cached, found := a.depotInfoCache[depotID]
	a.depotInfoCacheMu.RUnlock()
	if found && cached != nil {
		if cached.Platform == "PC" || cached.Platform == "" {
			if fresh, err := a.fetchRemoteSteamDepotInfo(depotID, userAgent); err == nil && fresh != nil && fresh.Platform != "PC" && fresh.Platform != "" {
				return *fresh
			}
		}
		a.triggerBackgroundDoubleCheck(depotID, userAgent)
		return *cached
	}

	// 2. Database lookup
	if a.db != nil {
		if meta, err := a.db.GetDepotMeta(depotID); err == nil && meta != nil && meta.GameTitle != "" {
			platform := meta.Platform
			if platform == "PC" || platform == "" {
				if fresh, err := a.fetchRemoteSteamDepotInfo(depotID, userAgent); err == nil && fresh != nil && fresh.Platform != "PC" && fresh.Platform != "" {
					return *fresh
				}
			}

			info := ports.DepotInfo{
				DepotID:   depotID,
				GameTitle: meta.GameTitle,
				AppID:     meta.AppID,
				Platform:  platform,
				TotalSize: meta.TotalSize,
			}
			a.depotInfoCacheMu.Lock()
			a.depotInfoCache[depotID] = &info
			a.depotInfoCacheMu.Unlock()

			// Double check with remote data asynchronously
			a.triggerBackgroundDoubleCheck(depotID, userAgent)
			return info
		}
	}

	// 3. Fresh remote lookup (synchronous initial fetch with timeout)
	if fresh, err := a.fetchRemoteSteamDepotInfo(depotID, userAgent); err == nil && fresh != nil {
		return *fresh
	}

	// 4. Fallback if offline
	baseAppID := a.ResolveBaseAppID(depotID)
	platform := FormatPlatform("", "", userAgent)
	return ports.DepotInfo{
		DepotID:   depotID,
		GameTitle: fmt.Sprintf("Steam App %d", baseAppID),
		AppID:     baseAppID,
		Platform:  platform,
		TotalSize: 0,
	}
}

func (a *Adapter) triggerBackgroundDoubleCheck(depotID int64, userAgent string) {
	a.depotFetchingMu.Lock()
	if a.depotFetching[depotID] {
		a.depotFetchingMu.Unlock()
		return
	}
	a.depotFetching[depotID] = true
	a.depotFetchingMu.Unlock()

	go func() {
		defer func() {
			a.depotFetchingMu.Lock()
			delete(a.depotFetching, depotID)
			a.depotFetchingMu.Unlock()
		}()

		_, _ = a.fetchRemoteSteamDepotInfo(depotID, userAgent)
	}()
}

// fetchRemoteSteamDepotInfo contacts remote Steam services to look up fresh app title, platform, and sizes
func (a *Adapter) fetchRemoteSteamDepotInfo(depotID int64, userAgent string) (*ports.DepotInfo, error) {
	if depotID <= 0 {
		return nil, fmt.Errorf("invalid depot ID")
	}

	baseAppID := a.ResolveBaseAppID(depotID)

	if a.db != nil {
		var existingAppID int64
		_ = a.db.QueryRow("SELECT app_id FROM depot_meta_cache WHERE depot_id = ? AND app_id > 0 LIMIT 1", depotID).Scan(&existingAppID)
		if existingAppID > 0 {
			baseAppID = existingAppID
		} else {
			_ = a.db.QueryRow("SELECT app_id FROM chunk_cache WHERE depot_id = ? AND app_id > 0 LIMIT 1", depotID).Scan(&existingAppID)
			if existingAppID > 0 {
				baseAppID = existingAppID
			}
		}
	}

	client := &http.Client{Timeout: 4 * time.Second}

	// 1. SteamCMD PICS endpoint (provides name, full depot manifests, oslist, osarch)
	apiURL := fmt.Sprintf("https://api.steamcmd.net/v1/info/%d", baseAppID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Artesian/1.0 (Game Cache Proxy)")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()

			var steamCmdData struct {
				Status string `json:"status"`
				Data   map[string]struct {
					Common struct {
						Name   string `json:"name"`
						Type   string `json:"type"`
						Parent string `json:"parent"`
					} `json:"common"`
					Extended struct {
						DlcForAppId string `json:"dlcforappid"`
					} `json:"extended"`
					Depots map[string]json.RawMessage `json:"depots"`
				} `json:"data"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&steamCmdData); err == nil {
				appKey := fmt.Sprintf("%d", baseAppID)
				if appData, ok := steamCmdData.Data[appKey]; ok && appData.Common.Name != "" {
					gameTitle := strings.TrimSpace(appData.Common.Name)

					var matchedInfo *ports.DepotInfo

					// Learn and store ALL sibling depots in SQLite
					for dKey, dRaw := range appData.Depots {
						dID, err := strconv.ParseInt(dKey, 10, 64)
						if err != nil {
							continue // skip non-numeric keys like baselanguages, branches, etc.
						}

						var dVal struct {
							Config struct {
								Oslist string `json:"oslist"`
								Osarch string `json:"osarch"`
							} `json:"config"`
							Manifests map[string]struct {
								Download string `json:"download"`
								Size     string `json:"size"`
							} `json:"manifests"`
						}
						if err := json.Unmarshal(dRaw, &dVal); err != nil {
							continue
						}

						platform := FormatPlatform(dVal.Config.Oslist, dVal.Config.Osarch, userAgent)

						var totalSize int64
						if pub, ok := dVal.Manifests["public"]; ok {
							if sz, err := strconv.ParseInt(pub.Download, 10, 64); err == nil && sz > 0 {
								totalSize = sz
							} else if sz, err := strconv.ParseInt(pub.Size, 10, 64); err == nil {
								totalSize = sz
							}
						}
						if totalSize == 0 {
							for _, m := range dVal.Manifests {
								if sz, err := strconv.ParseInt(m.Download, 10, 64); err == nil && sz > 0 {
									totalSize = sz
									break
								} else if sz, err := strconv.ParseInt(m.Size, 10, 64); err == nil && sz > 0 {
									totalSize = sz
									break
								}
							}
						}

						info := &ports.DepotInfo{
							DepotID:   dID,
							GameTitle: gameTitle,
							AppID:     baseAppID,
							Platform:  platform,
							TotalSize: totalSize,
						}

						a.depotInfoCacheMu.Lock()
						a.depotInfoCache[dID] = info
						a.depotInfoCacheMu.Unlock()

						if a.db != nil {
							_ = a.db.UpsertDepotMeta(db.DepotMeta{
								DepotID:   dID,
								AppID:     baseAppID,
								Service:   "steam",
								GameTitle: gameTitle,
								Platform:  platform,
								TotalSize: totalSize,
							})
						}

						if dID == depotID {
							matchedInfo = info
						}
					}

					// If the depot was not defined directly in this app, check parent
					if matchedInfo == nil {
						var parentAppID int64
						if appData.Common.Parent != "" {
							parentAppID, _ = strconv.ParseInt(appData.Common.Parent, 10, 64)
						}
						if parentAppID <= 0 && appData.Extended.DlcForAppId != "" {
							parentAppID, _ = strconv.ParseInt(appData.Extended.DlcForAppId, 10, 64)
						}

						if parentAppID > 0 {
							parentURL := fmt.Sprintf("https://api.steamcmd.net/v1/info/%d", parentAppID)
							pReq, pErr := http.NewRequest("GET", parentURL, nil)
							if pErr == nil {
								pReq.Header.Set("User-Agent", "Artesian/1.0 (Game Cache Proxy)")
								pResp, pErr := client.Do(pReq)
								if pErr == nil && pResp.StatusCode == http.StatusOK {
									defer pResp.Body.Close()
									var parentCmdData struct {
										Data map[string]struct {
											Common struct {
												Name string `json:"name"`
											} `json:"common"`
											Depots map[string]json.RawMessage `json:"depots"`
										} `json:"data"`
									}
									if err := json.NewDecoder(pResp.Body).Decode(&parentCmdData); err == nil {
										pKey := fmt.Sprintf("%d", parentAppID)
										if pData, ok := parentCmdData.Data[pKey]; ok {
											for dKey, dRaw := range pData.Depots {
												dID, err := strconv.ParseInt(dKey, 10, 64)
												if err != nil {
													continue
												}

												var dVal struct {
													Config struct {
														Oslist string `json:"oslist"`
														Osarch string `json:"osarch"`
													} `json:"config"`
													DlcAppID  string `json:"dlcappid"`
													Manifests map[string]struct {
														Gid      string `json:"gid"`
														Download string `json:"download"`
														Size     string `json:"size"`
													} `json:"manifests"`
												}
												if err := json.Unmarshal(dRaw, &dVal); err != nil {
													continue
												}

												depotAppID := parentAppID
												depotTitle := pData.Common.Name
												if dVal.DlcAppID != "" {
													if parsedDlc, err := strconv.ParseInt(dVal.DlcAppID, 10, 64); err == nil {
														depotAppID = parsedDlc
														if parsedDlc == baseAppID {
															depotTitle = gameTitle
														}
													}
												}

												platform := FormatPlatform(dVal.Config.Oslist, dVal.Config.Osarch, userAgent)
												var totalSize int64
												if pub, ok := dVal.Manifests["public"]; ok {
													if sz, err := strconv.ParseInt(pub.Download, 10, 64); err == nil && sz > 0 {
														totalSize = sz
													} else if sz, err := strconv.ParseInt(pub.Size, 10, 64); err == nil {
														totalSize = sz
													}
												}
												if totalSize == 0 {
													for _, m := range dVal.Manifests {
														if sz, err := strconv.ParseInt(m.Download, 10, 64); err == nil && sz > 0 {
															totalSize = sz
															break
														} else if sz, err := strconv.ParseInt(m.Size, 10, 64); err == nil && sz > 0 {
															totalSize = sz
															break
														}
													}
												}

												var manifestID int64
												if pub, ok := dVal.Manifests["public"]; ok {
													if gid, err := strconv.ParseInt(pub.Gid, 10, 64); err == nil {
														manifestID = gid
													}
												}
												if manifestID == 0 {
													for _, m := range dVal.Manifests {
														if gid, err := strconv.ParseInt(m.Gid, 10, 64); err == nil && gid > 0 {
															manifestID = gid
															break
														}
													}
												}

												info := &ports.DepotInfo{
													DepotID:    dID,
													GameTitle:  depotTitle,
													AppID:      depotAppID,
													Platform:   platform,
													TotalSize:  totalSize,
													ManifestID: manifestID,
												}

												a.depotInfoCacheMu.Lock()
												a.depotInfoCache[dID] = info
												a.depotInfoCacheMu.Unlock()

												if a.db != nil {
													_ = a.db.UpsertDepotMeta(db.DepotMeta{
														DepotID:    dID,
														AppID:      depotAppID,
														Service:    "steam",
														GameTitle:  depotTitle,
														Platform:   platform,
														TotalSize:  totalSize,
														ManifestID: manifestID,
													})
												}

												if dID == depotID {
													matchedInfo = info
												}
											}
										}
									}
								}
							}
						}
					}

					if matchedInfo != nil {
						return matchedInfo, nil
					}

					info := &ports.DepotInfo{
						DepotID:   depotID,
						GameTitle: gameTitle,
						AppID:     baseAppID,
						Platform:  FormatPlatform("", "", userAgent),
						TotalSize: 0,
					}
					return info, nil
				}
			}
		}
	}

	// 2. Fallback to Steam Store Web API
	storeURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d", baseAppID)
	storeReq, _ := http.NewRequest("GET", storeURL, nil)
	storeReq.Header.Set("User-Agent", "Artesian/1.0 (Game Cache Proxy)")
	storeResp, err := client.Do(storeReq)
	if err == nil && storeResp.StatusCode == http.StatusOK {
		defer storeResp.Body.Close()

		var storeData map[string]struct {
			Success bool `json:"success"`
			Data    struct {
				Name      string `json:"name"`
				Platforms struct {
					Windows bool `json:"windows"`
					Mac     bool `json:"mac"`
					Linux   bool `json:"linux"`
				} `json:"platforms"`
			} `json:"data"`
		}

		if err := json.NewDecoder(storeResp.Body).Decode(&storeData); err == nil {
			if entry, ok := storeData[fmt.Sprintf("%d", baseAppID)]; ok && entry.Success && entry.Data.Name != "" {
				name := strings.TrimSpace(entry.Data.Name)
				platform := FormatPlatform("", "", userAgent)
				if platform == "PC" {
					if entry.Data.Platforms.Mac && !entry.Data.Platforms.Windows {
						platform = "Mac 64bit"
					} else if entry.Data.Platforms.Windows && !entry.Data.Platforms.Mac && !entry.Data.Platforms.Linux {
						platform = "Windows"
					}
				}

				info := &ports.DepotInfo{
					DepotID:   depotID,
					GameTitle: name,
					AppID:     baseAppID,
					Platform:  platform,
					TotalSize: 0,
				}

				a.depotInfoCacheMu.Lock()
				a.depotInfoCache[depotID] = info
				a.depotInfoCacheMu.Unlock()

				if a.db != nil {
					_ = a.db.UpsertDepotMeta(db.DepotMeta{
						DepotID:   depotID,
						AppID:     baseAppID,
						Service:   "steam",
						GameTitle: name,
						Platform:  platform,
						TotalSize: 0,
					})
				}
				return info, nil
			}
		}
	}

	return nil, fmt.Errorf("remote lookup failed")
}

// FetchAppInfo queries SteamCMD PICS endpoint to populate depot_meta_cache with all depots and sizes for an App ID
func (a *Adapter) FetchAppInfo(appID int64) error {
	if appID <= 0 {
		return fmt.Errorf("invalid app ID: %d", appID)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	apiURL := fmt.Sprintf("https://api.steamcmd.net/v1/info/%d", appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Artesian/1.0 (Game Cache Proxy)")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Depot Meta] Error fetching SteamCMD info for app %d: %v", appID, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Depot Meta] SteamCMD API returned HTTP %d for app %d", resp.StatusCode, appID)
		return fmt.Errorf("SteamCMD API returned HTTP %d for app %d", resp.StatusCode, appID)
	}

	var steamCmdData struct {
		Status string `json:"status"`
		Data   map[string]struct {
			Common struct {
				Name   string `json:"name"`
				Type   string `json:"type"`
				Parent string `json:"parent"`
			} `json:"common"`
			Extended struct {
				DlcForAppId string `json:"dlcforappid"`
			} `json:"extended"`
			Depots map[string]json.RawMessage `json:"depots"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&steamCmdData); err != nil {
		log.Printf("[Depot Meta] Failed to decode SteamCMD JSON for app %d: %v", appID, err)
		return err
	}

	appKey := fmt.Sprintf("%d", appID)
	appData, ok := steamCmdData.Data[appKey]
	if !ok || len(appData.Depots) == 0 {
		return fmt.Errorf("no depot data found for app %d", appID)
	}

	gameTitle := strings.TrimSpace(appData.Common.Name)

	for dKey, dRaw := range appData.Depots {
		dID, err := strconv.ParseInt(dKey, 10, 64)
		if err != nil {
			continue // skip non-numeric keys like branches, baselanguages
		}

		var dVal struct {
			Config struct {
				Oslist string `json:"oslist"`
				Osarch string `json:"osarch"`
			} `json:"config"`
			Manifests map[string]struct {
				Gid      string `json:"gid"`
				Download string `json:"download"`
				Size     string `json:"size"`
			} `json:"manifests"`
		}
		if err := json.Unmarshal(dRaw, &dVal); err != nil {
			continue
		}

		platform := FormatPlatform(dVal.Config.Oslist, dVal.Config.Osarch, "")

		var manifestID int64
		if pub, ok := dVal.Manifests["public"]; ok {
			if gid, err := strconv.ParseUint(pub.Gid, 10, 64); err == nil {
				manifestID = int64(gid)
			}
		}
		if manifestID == 0 {
			for _, m := range dVal.Manifests {
				if gid, err := strconv.ParseUint(m.Gid, 10, 64); err == nil && gid > 0 {
					manifestID = int64(gid)
					break
				}
			}
		}

		var totalSize int64
		if pub, ok := dVal.Manifests["public"]; ok {
			if sz, err := strconv.ParseInt(pub.Download, 10, 64); err == nil && sz > 0 {
				totalSize = sz
			} else if sz, err := strconv.ParseInt(pub.Size, 10, 64); err == nil {
				totalSize = sz
			}
		}
		if totalSize == 0 {
			for _, m := range dVal.Manifests {
				if sz, err := strconv.ParseInt(m.Download, 10, 64); err == nil && sz > 0 {
					totalSize = sz
					break
				} else if sz, err := strconv.ParseInt(m.Size, 10, 64); err == nil && sz > 0 {
					totalSize = sz
					break
				}
			}
		}

		info := &ports.DepotInfo{
			DepotID:    dID,
			GameTitle:  gameTitle,
			AppID:      appID,
			Platform:   platform,
			TotalSize:  totalSize,
			ManifestID: manifestID,
		}

		a.depotInfoCacheMu.Lock()
		a.depotInfoCache[dID] = info
		a.depotInfoCacheMu.Unlock()

		if a.db != nil {
			_ = a.db.UpsertDepotMeta(db.DepotMeta{
				DepotID:    dID,
				AppID:      appID,
				Service:    "steam",
				GameTitle:  gameTitle,
				Platform:   platform,
				TotalSize:  totalSize,
				ManifestID: manifestID,
			})
		}
	}

	return nil
}
