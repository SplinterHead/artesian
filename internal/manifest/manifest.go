package manifest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"artesian/internal/db"
)

func readVarint(data []byte, offset int) (uint64, int, error) {
	var res uint64
	var shift uint
	for offset < len(data) {
		b := data[offset]
		offset++
		res |= uint64(b&0x7f) << shift
		if (b & 0x80) == 0 {
			return res, offset, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, offset, fmt.Errorf("varint overflow")
		}
	}
	return 0, offset, fmt.Errorf("unexpected EOF")
}

func skipField(wire int, data []byte, offset int) (int, error) {
	switch wire {
	case 0:
		_, off, err := readVarint(data, offset)
		return off, err
	case 1:
		if offset+8 > len(data) {
			return offset, fmt.Errorf("EOF fixed64")
		}
		return offset + 8, nil
	case 2:
		length, off, err := readVarint(data, offset)
		if err != nil {
			return off, err
		}
		if off+int(length) > len(data) {
			return off, fmt.Errorf("EOF length delimited")
		}
		return off + int(length), nil
	case 5:
		if offset+4 > len(data) {
			return offset, fmt.Errorf("EOF fixed32")
		}
		return offset + 4, nil
	default:
		return offset, fmt.Errorf("unknown wire type %d", wire)
	}
}

// ParseManifestFile reads and parses a Steam depot manifest file from disk
func ParseManifestFile(path string) ([]db.ManifestChunkRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseManifestBytes(data)
}

// ParseManifestBytes parses Steam manifest container (zip with magic 0x71F617D0) into chunk records
func ParseManifestBytes(data []byte) ([]db.ManifestChunkRecord, error) {
	var rawPayload []byte

	// 1. Check if ZIP archive (PK\x03\x04)
	if len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to open manifest zip: %w", err)
		}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			rawPayload, err = io.ReadAll(rc)
			_ = rc.Close()
			if err == nil && len(rawPayload) > 8 {
				break
			}
		}
	} else {
		rawPayload = data
	}

	if len(rawPayload) < 8 {
		return nil, fmt.Errorf("manifest payload too short (%d bytes)", len(rawPayload))
	}

	magic := binary.LittleEndian.Uint32(rawPayload[0:4])
	if magic != 0x71F617D0 {
		return nil, fmt.Errorf("unsupported manifest magic 0x%08x", magic)
	}

	payloadLen := binary.LittleEndian.Uint32(rawPayload[4:8])
	if int(8+payloadLen) > len(rawPayload) {
		payloadLen = uint32(len(rawPayload) - 8)
	}
	payload := rawPayload[8 : 8+payloadLen]

	var chunks []db.ManifestChunkRecord
	offset := 0
	for offset < len(payload) {
		tag, off, err := readVarint(payload, offset)
		if err != nil {
			break
		}
		offset = off
		wire := int(tag & 7)
		field := int(tag >> 3)

		if wire == 2 && field == 1 { // FileMapping
			mLen, off, err := readVarint(payload, offset)
			if err != nil {
				break
			}
			mEnd := off + int(mLen)
			mOff := off
			offset = mEnd

			for mOff < mEnd {
				mTag, nextOff, err := readVarint(payload, mOff)
				if err != nil {
					break
				}
				mOff = nextOff
				mWire := int(mTag & 7)
				mField := int(mTag >> 3)

				if mWire == 2 && mField == 6 { // repeated ChunkData chunks = 6
					cLen, nextOff, err := readVarint(payload, mOff)
					if err != nil {
						break
					}
					cEnd := nextOff + int(cLen)
					cOff := nextOff
					mOff = cEnd

					var chunkSHA string
					var cOrig, cComp uint32

					for cOff < cEnd {
						cTag, nextOff, err := readVarint(payload, cOff)
						if err != nil {
							break
						}
						cOff = nextOff
						cWire := int(cTag & 7)
						cField := int(cTag >> 3)

						if cWire == 2 && cField == 1 { // sha (20-byte SHA-1 hash)
							sLen, nextOff, err := readVarint(payload, cOff)
							if err != nil {
								break
							}
							if nextOff+int(sLen) <= len(payload) {
								chunkSHA = hex.EncodeToString(payload[nextOff : nextOff+int(sLen)])
							}
							cOff = nextOff + int(sLen)
						} else if cWire == 0 && cField == 4 { // cb_original
							val, nextOff, err := readVarint(payload, cOff)
							if err != nil {
								break
							}
							cOrig = uint32(val)
							cOff = nextOff
						} else if cWire == 0 && cField == 5 { // cb_compressed
							val, nextOff, err := readVarint(payload, cOff)
							if err != nil {
								break
							}
							cComp = uint32(val)
							cOff = nextOff
						} else {
							cOff, err = skipField(cWire, payload, cOff)
							if err != nil {
								break
							}
						}
					}

					if chunkSHA != "" {
						size := int64(cComp)
						if size == 0 {
							size = int64(cOrig)
						}
						chunks = append(chunks, db.ManifestChunkRecord{
							ChunkID:        chunkSHA,
							SizeBytes:      size,
							OriginalSize:   int64(cOrig),
							CompressedSize: int64(cComp),
						})
					}
				} else {
					mOff, err = skipField(mWire, payload, mOff)
					if err != nil {
						break
					}
				}
			}
		} else {
			offset, err = skipField(wire, payload, offset)
			if err != nil {
				break
			}
		}
	}

	return chunks, nil
}

// ExtractManifestDepotID extracts the depot ID and manifest GID from the manifest metadata trailer
func ExtractManifestDepotID(data []byte) (int64, int64, error) {
	var rawPayload []byte
	if len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return 0, 0, err
		}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			rawPayload, err = io.ReadAll(rc)
			_ = rc.Close()
			if err == nil && len(rawPayload) > 8 {
				break
			}
		}
	} else {
		rawPayload = data
	}

	if len(rawPayload) < 8 {
		return 0, 0, fmt.Errorf("payload too short")
	}

	magic := binary.LittleEndian.Uint32(rawPayload[0:4])
	if magic != 0x71F617D0 {
		return 0, 0, fmt.Errorf("invalid manifest magic")
	}

	payloadLen := binary.LittleEndian.Uint32(rawPayload[4:8])
	metaStart := 8 + int(payloadLen)
	if len(rawPayload) < metaStart+8 {
		return 0, 0, fmt.Errorf("no metadata trailer")
	}

	metaMagic := binary.LittleEndian.Uint32(rawPayload[metaStart : metaStart+4])
	if metaMagic != 0x1F4812BE {
		return 0, 0, fmt.Errorf("invalid metadata magic 0x%08x", metaMagic)
	}

	metaLen := int(binary.LittleEndian.Uint32(rawPayload[metaStart+4 : metaStart+8]))
	if metaStart+8+metaLen > len(rawPayload) {
		metaLen = len(rawPayload) - (metaStart + 8)
	}
	meta := rawPayload[metaStart+8 : metaStart+8+metaLen]

	var depotID, manifestGID int64
	off := 0
	for off < len(meta) {
		tag, nextOff, err := readVarint(meta, off)
		if err != nil {
			break
		}
		field := int(tag >> 3)
		val, nextOff, err := readVarint(meta, nextOff)
		if err != nil {
			break
		}
		off = nextOff
		if field == 1 {
			depotID = int64(val)
		} else if field == 2 {
			manifestGID = int64(val)
		}
	}

	if depotID == 0 {
		return 0, 0, fmt.Errorf("depot id not found in manifest metadata")
	}
	return depotID, manifestGID, nil
}

// FindAndParseDepotManifest searches configured manifest directories for a depot manifest and parses it
func FindAndParseDepotManifest(storageDir, manifestDir, depotCacheDir string, depotID, manifestID int64) ([]db.ManifestChunkRecord, string, error) {
	if storageDir == "" {
		storageDir = "/data/cache"
	}
	if manifestDir == "" {
		manifestDir = "/data/manifests"
	}
	if depotCacheDir == "" {
		depotCacheDir = "/data/steam_depotcache"
	}

	candidateDirs := []string{
		depotCacheDir,
		manifestDir,
		filepath.Join(storageDir, "manifests"),
	}

	// 1. Direct file check with exact manifestID
	if manifestID > 0 {
		exactName := fmt.Sprintf("%d_%d.manifest", depotID, manifestID)
		for _, dir := range candidateDirs {
			target := filepath.Join(dir, exactName)
			if info, err := os.Stat(target); err == nil && !info.IsDir() && info.Size() > 0 {
				chunks, err := ParseManifestFile(target)
				if err == nil && len(chunks) > 0 {
					log.Printf("[Manifest Parser] Found exact manifest for depot %d at %s (%d chunks)", depotID, target, len(chunks))
					return chunks, target, nil
				}
			}
		}
	}

	// 2. Wildcard search for any depot manifest: <depotID>_*.manifest
	pattern := fmt.Sprintf("%d_*.manifest", depotID)
	for _, dir := range candidateDirs {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err == nil && len(matches) > 0 {
			// Sort so latest / newest manifest is tried first
			for i := len(matches) - 1; i >= 0; i-- {
				target := matches[i]
				chunks, err := ParseManifestFile(target)
				if err == nil && len(chunks) > 0 {
					log.Printf("[Manifest Parser] Found wildcard manifest for depot %d at %s (%d chunks)", depotID, target, len(chunks))
					return chunks, target, nil
				}
			}
		}
	}

	// 3. Scan cached files in storageDir/steam (e.g. manifests intercepted by the proxy)
	cDir := filepath.Join(storageDir, "steam")
	if _, err := os.Stat(cDir); err == nil {
		var foundChunks []db.ManifestChunkRecord
		var foundPath string
		_ = filepath.Walk(cDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() || info.Size() < 5000 {
				return nil
			}
			data, rErr := os.ReadFile(path)
			if rErr != nil {
				return nil
			}
			// Strictly verify that the manifest belongs to requested depotID
			candDepot, candGID, mErr := ExtractManifestDepotID(data)
			if mErr == nil && candDepot == depotID {
				if manifestID == 0 || candGID == manifestID {
					chunks, pErr := ParseManifestBytes(data)
					if pErr == nil && len(chunks) > 0 {
						foundChunks = chunks
						foundPath = path
						return filepath.SkipAll
					}
				}
			}
			return nil
		})
		if len(foundChunks) > 0 {
			log.Printf("[Manifest Parser] Extracted %d chunks from matching cached manifest at %s for depot %d", len(foundChunks), foundPath, depotID)
			return foundChunks, foundPath, nil
		}
	}

	return nil, "", fmt.Errorf("no valid manifest found on disk for depot %d (manifest %d)", depotID, manifestID)
}

// ParseManifestDepotFromURL extracts depot ID and manifest ID from Steam manifest request URL
func ParseManifestDepotFromURL(urlPath string) (int64, int64, bool) {
	// Pattern: /depot/(\d+)/manifest/(\d+)
	idx := strings.Index(urlPath, "/depot/")
	if idx == -1 {
		return 0, 0, false
	}
	parts := strings.Split(strings.Trim(urlPath[idx+7:], "/"), "/")
	if len(parts) >= 3 && parts[1] == "manifest" {
		depotID, err1 := strconv.ParseInt(parts[0], 10, 64)
		manifestID, err2 := strconv.ParseInt(parts[2], 10, 64)
		if err1 == nil && err2 == nil {
			return depotID, manifestID, true
		}
	}
	return 0, 0, false
}

// SaveRawManifest writes an intercepted raw manifest payload to disk in the storage directory
func SaveRawManifest(storageDir string, depotID, manifestID int64, data []byte) string {
	if storageDir == "" {
		storageDir = "/data/cache"
	}
	targetDir := filepath.Join(storageDir, "manifests")
	if err := os.MkdirAll(targetDir, 0755); err == nil {
		filePath := filepath.Join(targetDir, fmt.Sprintf("%d_%d.manifest", depotID, manifestID))
		if err := os.WriteFile(filePath, data, 0644); err == nil {
			return filePath
		}
	}
	return ""
}

// DeleteManifestFiles removes all manifest files on disk for a given depot ID from the storage directory
func DeleteManifestFiles(storageDir string, depotID int64) error {
	if storageDir == "" {
		storageDir = "/data/cache"
	}
	targetDir := filepath.Join(storageDir, "manifests")
	pattern := fmt.Sprintf("%d_*.manifest", depotID)
	matches, err := filepath.Glob(filepath.Join(targetDir, pattern))
	if err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
	return nil
}

