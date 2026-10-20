package steam

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"artesian/internal/ports"
)

// CheckAppUpdate checks SteamCMD PICS API for changes against the current recorded change number and manifests
func (a *Adapter) CheckAppUpdate(appID int64, currentChangeNumber uint64) (*ports.AppUpdateCheck, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("invalid app ID: %d", appID)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	apiURL := fmt.Sprintf("https://api.steamcmd.net/v1/info/%d", appID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request for app %d: %w", appID, err)
	}
	req.Header.Set("User-Agent", "Artesian/1.0 (Game Cache Proxy)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network check failed for app %d: %w", appID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SteamCMD API returned HTTP %d for app %d", resp.StatusCode, appID)
	}

	var steamCmdData struct {
		Status string `json:"status"`
		Data   map[string]struct {
			ChangeNumber uint64 `json:"_change_number"`
			Common       struct {
				Name string `json:"name"`
			} `json:"common"`
			Depots map[string]json.RawMessage `json:"depots"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&steamCmdData); err != nil {
		return nil, fmt.Errorf("failed to decode SteamCMD response for app %d: %w", appID, err)
	}

	appKey := fmt.Sprintf("%d", appID)
	appData, ok := steamCmdData.Data[appKey]
	if !ok {
		return nil, fmt.Errorf("no app data returned for app %d", appID)
	}

	liveChangeNumber := appData.ChangeNumber
	appName := appData.Common.Name

	// Check if any public branch manifest GID has changed
	manifestChanged := false
	for dKey, dRaw := range appData.Depots {
		dID, err := strconv.ParseInt(dKey, 10, 64)
		if err != nil {
			continue
		}
		var dVal struct {
			Manifests map[string]struct {
				Gid string `json:"gid"`
			} `json:"manifests"`
		}
		if err := json.Unmarshal(dRaw, &dVal); err != nil {
			continue
		}
		if pub, ok := dVal.Manifests["public"]; ok {
			if gid, err := strconv.ParseInt(pub.Gid, 10, 64); err == nil && gid > 0 {
				if a.db != nil {
					existingMeta, _ := a.db.GetDepotMeta(dID)
					if existingMeta != nil && existingMeta.ManifestID > 0 && existingMeta.ManifestID != gid {
						manifestChanged = true
					}
				}
			}
		}
	}

	// Baseline initialization: if app has no recorded change number (0)
	if currentChangeNumber == 0 {
		_ = a.FetchAppInfo(appID)
		return &ports.AppUpdateCheck{
			AppID:           appID,
			Name:            appName,
			OldChangeNumber: 0,
			NewChangeNumber: liveChangeNumber,
			Updated:         false,
			ManifestChanged: false,
		}, nil
	}

	hasUpdate := (liveChangeNumber > currentChangeNumber) || manifestChanged
	if hasUpdate {
		// Automatically refresh depot metadata in SQLite
		_ = a.FetchAppInfo(appID)
		log.Printf("[Steam Update Check] UPDATE DETECTED for %s (App %d): Change #%d -> #%d (Manifest changed: %v)",
			appName, appID, currentChangeNumber, liveChangeNumber, manifestChanged)
	}

	return &ports.AppUpdateCheck{
		AppID:           appID,
		Name:            appName,
		OldChangeNumber: currentChangeNumber,
		NewChangeNumber: liveChangeNumber,
		Updated:         hasUpdate,
		ManifestChanged: manifestChanged,
	}, nil
}
