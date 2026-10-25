package xbox

import (
	"fmt"

	"artesian/internal/ports"
)

// ResolveDepot returns metadata for an active Xbox game or download session
func (a *Adapter) ResolveDepot(depotID int64, userAgent string) ports.DepotInfo {
	a.pkgMu.RLock()
	pkg, ok := a.packageMap[depotID]
	a.pkgMu.RUnlock()

	if ok && pkg != nil {
		plat := "Windows"
		if len(pkg.Platforms) > 0 {
			plat = pkg.Platforms[0]
		}
		return ports.DepotInfo{
			DepotID:    depotID,
			AppID:      depotID,
			GameTitle:  pkg.Title,
			Platform:   plat,
			TotalSize:  pkg.PackageSize,
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
		GameTitle: fmt.Sprintf("Xbox Package %d", depotID),
		Platform:  "Windows",
	}
}

// ResolveBaseAppID maps a depot or package asset ID back to its parent game app ID
func (a *Adapter) ResolveBaseAppID(depotID int64) int64 {
	a.pkgMu.RLock()
	defer a.pkgMu.RUnlock()

	if _, ok := a.packageMap[depotID]; ok {
		return depotID
	}
	return depotID
}

// FetchAppInfo populates metadata for an app from Display Catalog and resolves genuine package URLs & sizes
func (a *Adapter) FetchAppInfo(appID int64) error {
	a.pkgMu.RLock()
	prodID, ok := a.appToProdID[appID]
	a.pkgMu.RUnlock()

	if !ok || prodID == "" {
		// Look up in SQLite game_meta_cache
		if a.db != nil {
			var id string
			err := a.db.QueryRow("SELECT id FROM game_meta_cache WHERE app_id = ? AND service = 'xbox' LIMIT 1", appID).Scan(&id)
			if err == nil && id != "" {
				prodID = id
				a.pkgMu.Lock()
				a.prodToAppID[prodID] = appID
				a.appToProdID[appID] = prodID
				a.pkgMu.Unlock()
			}
		}
	}

	if prodID == "" {
		return fmt.Errorf("no Microsoft Product ID mapped for app %d", appID)
	}

	results := a.hydrateProducts([]string{prodID})
	if len(results) == 0 {
		return fmt.Errorf("could not hydrate product %s from Display Catalog", prodID)
	}

	// Crucial: Resolve genuine package URLs, sizes, and depot metadata from Xbox Package Service
	if _, err := a.ResolvePackage(appID); err != nil {
		return fmt.Errorf("could not resolve package download metadata for app %d (%s): %w", appID, prodID, err)
	}

	return nil
}

// CheckAppUpdate checks for new package updates for a watched Xbox game
func (a *Adapter) CheckAppUpdate(appID int64, currentChangeNumber uint64) (*ports.AppUpdateCheck, error) {
	pkg, err := a.ResolvePackage(appID)
	if err != nil {
		return nil, err
	}

	// For Xbox monolithic archives, change detection is driven by package size or content ID hash
	newChange := uint64(pkg.PackageSize)
	if newChange == 0 {
		newChange = 1
	}

	title := pkg.Title
	if title == "" {
		title = fmt.Sprintf("Xbox Title %d", appID)
	}

	updated := currentChangeNumber > 0 && newChange != currentChangeNumber

	return &ports.AppUpdateCheck{
		AppID:           appID,
		Name:            title,
		OldChangeNumber: currentChangeNumber,
		NewChangeNumber: newChange,
		Updated:         updated,
		ManifestChanged: updated,
	}, nil
}
