package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"artesian/internal/config"
)

var steamChunkRegex = regexp.MustCompile(`/depot/(\d+)/chunk/([a-fA-F0-9]{40})`)

// Storage handles local disk caching of content chunks
type Storage struct {
	baseDir       string
	manifestDir   string
	depotCacheDir string
	maxSize       int64
	minFreeDisk   int64
	mu            sync.RWMutex

	// Live I/O tracking
	readBytesTotal  int64
	writeBytesTotal int64
	lastIOPeriod    int64
	lastReadBytes   int64
	lastWriteBytes  int64
	readRateSec     int64
	writeRateSec    int64
}

// NewStorage initializes local storage directory structure
func NewStorage(cfg config.CacheConfig) (*Storage, error) {
	if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory %s: %w", cfg.StorageDir, err)
	}

	manifestDir := cfg.ManifestDir
	if manifestDir == "" {
		manifestDir = "/data/manifests"
	}
	depotCacheDir := cfg.DepotCacheDir
	if depotCacheDir == "" {
		depotCacheDir = "/data/steam_depotcache"
	}

	s := &Storage{
		baseDir:       cfg.StorageDir,
		manifestDir:   manifestDir,
		depotCacheDir: depotCacheDir,
		maxSize:       cfg.MaxSizeBytes,
		minFreeDisk:   cfg.MinFreeDisk,
		lastIOPeriod:  time.Now().Unix(),
	}

	go s.ioRateMonitor()
	return s, nil
}

// SetBaseDir allows runtime reconfiguration of the storage pool path
func (s *Storage) SetBaseDir(newDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(newDir, 0755); err != nil {
		return err
	}
	s.baseDir = newDir
	return nil
}

// GetBaseDir returns current cache path
func (s *Storage) GetBaseDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseDir
}

// GetManifestDir returns the configured manifest pool directory
func (s *Storage) GetManifestDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manifestDir
}

// GetDepotCacheDir returns the mounted Steam depotcache directory
func (s *Storage) GetDepotCacheDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.depotCacheDir
}

// ParseSteamChunkExtract extracts depotID and chunkHash if URL is a Steam chunk
func ParseSteamChunkExtract(path string) (int64, string, bool) {
	matches := steamChunkRegex.FindStringSubmatch(path)
	if len(matches) == 3 {
		depotID, _ := strconv.ParseInt(matches[1], 10, 64)
		chunkHash := strings.ToLower(matches[2])
		return depotID, chunkHash, true
	}
	return 0, "", false
}

// KeyToPath computes the storage path on the tank for a given service and chunk key
func (s *Storage) KeyToPath(service, key string) string {
	s.mu.RLock()
	baseDir := s.baseDir
	s.mu.RUnlock()

	// If Steam chunk format /depot/<depot_id>/chunk/<chunk_hash>
	if service == "steam" {
		if depotID, chunkHash, ok := ParseSteamChunkExtract(key); ok {
			prefix1 := chunkHash[0:2]
			prefix2 := chunkHash[2:4]
			return filepath.Join(baseDir, "steam", fmt.Sprintf("%d", depotID), prefix1, prefix2, chunkHash)
		}
	}

	// Generic content hash sharding
	hash := sha256.Sum256([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	prefix1 := hashStr[0:2]
	prefix2 := hashStr[2:4]
	return filepath.Join(baseDir, service, prefix1, prefix2, hashStr)
}

// Has checks if a chunk already exists on the tank
func (s *Storage) Has(service, key string) bool {
	path := s.KeyToPath(service, key)
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Open reads a cached chunk and updates I/O tracking
func (s *Storage) Open(service, key string) (*os.File, int64, error) {
	path := s.KeyToPath(service, key)
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}

	// Record read byte count
	atomic.AddInt64(&s.readBytesTotal, info.Size())

	// Update access time for LRU tracking
	_ = os.Chtimes(path, time.Now(), time.Now())

	return file, info.Size(), nil
}

// Store atomically writes a chunk into the tank
func (s *Storage) Store(service, key string, src io.Reader) (string, int64, error) {
	destPath := s.KeyToPath(service, key)
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create tank chunk dir %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, "tank-chunk-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("failed to create temporary chunk file: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	written, err := io.Copy(tmpFile, src)
	if err != nil {
		return "", 0, fmt.Errorf("failed to write chunk payload to tank: %w", err)
	}

	_ = tmpFile.Close()

	if err := os.Rename(tmpFile.Name(), destPath); err != nil {
		return "", 0, fmt.Errorf("failed to commit chunk to tank: %w", err)
	}

	atomic.AddInt64(&s.writeBytesTotal, written)
	return destPath, written, nil
}

// GetIOStats returns current read and write rates in bytes per second
func (s *Storage) GetIOStats() (int64, int64) {
	return atomic.LoadInt64(&s.readRateSec), atomic.LoadInt64(&s.writeRateSec)
}

func (s *Storage) ioRateMonitor() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		nowRead := atomic.LoadInt64(&s.readBytesTotal)
		nowWrite := atomic.LoadInt64(&s.writeBytesTotal)

		diffRead := nowRead - s.lastReadBytes
		diffWrite := nowWrite - s.lastWriteBytes

		atomic.StoreInt64(&s.readRateSec, diffRead)
		atomic.StoreInt64(&s.writeRateSec, diffWrite)

		s.lastReadBytes = nowRead
		s.lastWriteBytes = nowWrite
	}
}

// DeleteDepot removes all stored chunk files and directory structure for a depot
func (s *Storage) DeleteDepot(service string, depotID int64) error {
	s.mu.RLock()
	baseDir := s.baseDir
	s.mu.RUnlock()

	depotDir := filepath.Join(baseDir, service, fmt.Sprintf("%d", depotID))
	return os.RemoveAll(depotDir)
}
