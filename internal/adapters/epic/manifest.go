package epic

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"artesian/internal/ports"
)

const (
	ManifestMagic = 0x44BEC00C
)

// ManifestHeader describes the 41-byte binary header of an Epic Games manifest
type ManifestHeader struct {
	Magic            uint32
	HeaderSize       uint32
	SizeUncompressed uint32
	SizeCompressed   uint32
	SHAHash          [20]byte
	StoredAs         uint8  // Bitmask: 0x01 = zlib compressed, 0x02 = encrypted
	Version          uint32 // Determines ChunkSubdir if featureLevel not set
}

// EpicChunk describes a chunk extracted from an Epic manifest
type EpicChunk struct {
	ChunkID      string // {Hash}_{GUID}
	GUID         string // 32 hex characters
	Hash         string // 16 hex characters (%016X)
	GroupNumber  string // %02d (e.g. "42")
	RelativePath string // {ChunksSubdir}/{GroupNumber}/{Hash}_{GUID}.chunk
	SizeBytes    int64  // File size in bytes
}

// ParsedManifest contains all chunks and metadata parsed from an Epic manifest
type ParsedManifest struct {
	AppID        int64
	AppName      string
	BuildVersion string
	FeatureLevel int32
	ChunksSubdir string // "Chunks", "ChunksV2", "ChunksV3", "ChunksV4"
	Chunks       []EpicChunk
	TotalSize    int64
}

// GetBaseDir extracts the base CDN directory from a manifest URI by removing the manifest filename
func GetBaseDir(manifestURI string) string {
	idx := strings.LastIndex(manifestURI, "/")
	if idx > 0 {
		return manifestURI[:idx]
	}
	return manifestURI
}

// ParseManifest parses either binary or legacy JSON Epic manifests
func ParseManifest(data []byte) (*ParsedManifest, error) {
	if len(data) >= 4 {
		magic := binary.LittleEndian.Uint32(data[0:4])
		if magic == ManifestMagic {
			return ParseBinaryManifest(data)
		}
	}
	// Check for gzip compression
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		if gr, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
			if uncompressed, err := io.ReadAll(gr); err == nil {
				_ = gr.Close()
				return ParseManifest(uncompressed)
			}
			_ = gr.Close()
		}
	}
	// Check for zlib compression
	if len(data) >= 2 && data[0] == 0x78 && (data[1] == 0x9c || data[1] == 0x01 || data[1] == 0xda || data[1] == 0x5e) {
		if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			if uncompressed, err := io.ReadAll(zr); err == nil {
				_ = zr.Close()
				return ParseManifest(uncompressed)
			}
			_ = zr.Close()
		}
	}
	return ParseJSONManifest(data)
}

// ParseBinaryManifest parses a zlib-compressed binary Epic manifest (.manifest)
func ParseBinaryManifest(data []byte) (*ParsedManifest, error) {
	if len(data) < 37 {
		return nil, fmt.Errorf("epic manifest data too short (%d bytes)", len(data))
	}

	header := ManifestHeader{
		Magic:            binary.LittleEndian.Uint32(data[0:4]),
		HeaderSize:       binary.LittleEndian.Uint32(data[4:8]),
		SizeUncompressed: binary.LittleEndian.Uint32(data[8:12]),
		SizeCompressed:   binary.LittleEndian.Uint32(data[12:16]),
		StoredAs:         data[36],
	}
	copy(header.SHAHash[:], data[16:36])

	if header.Magic != ManifestMagic {
		return nil, fmt.Errorf("invalid epic manifest magic: 0x%08X", header.Magic)
	}

	headerSize := int(header.HeaderSize)
	if headerSize < 37 || headerSize >= len(data) {
		return nil, fmt.Errorf("invalid header size %d for data length %d", headerSize, len(data))
	}

	if headerSize >= 41 && len(data) >= 41 {
		header.Version = binary.LittleEndian.Uint32(data[37:41])
	}

	compressedData := data[headerSize:]
	if header.SizeCompressed > 0 && int(header.SizeCompressed) <= len(compressedData) {
		compressedData = compressedData[:header.SizeCompressed]
	}

	var payload []byte
	if header.StoredAs&0x01 != 0 {
		zr, err := zlib.NewReader(bytes.NewReader(compressedData))
		if err != nil {
			return nil, fmt.Errorf("failed to open zlib reader for manifest: %w", err)
		}
		defer zr.Close()

		decompressed, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress epic manifest: %w", err)
		}
		payload = decompressed
	} else {
		payload = compressedData
	}

	return parseDecompressedPayload(payload, header.Version)
}

func parseDecompressedPayload(payload []byte, headerVersion uint32) (*ParsedManifest, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("decompressed manifest payload too short (%d bytes)", len(payload))
	}

	// 1. Section 1: ManifestMeta
	// In the Epic binary manifest format, the first 4 bytes indicate the total size of Section 1
	// measured from offset 0, meaning Section 2 begins at exactly metaSize.
	metaSize := int(binary.LittleEndian.Uint32(payload[0:4]))
	if metaSize < 4 || metaSize > len(payload) {
		return nil, fmt.Errorf("invalid meta section size %d (payload len %d)", metaSize, len(payload))
	}

	featureLevel := int32(headerVersion)
	// payload[4]: dataVersion (EManifestMetaVersion)
	// payload[5:9]: featureLevel (int32)
	if metaSize >= 9 {
		fl := int32(binary.LittleEndian.Uint32(payload[5:9]))
		if fl > 0 {
			featureLevel = fl
		}
	}

	// Determine chunk subdirectory based on FeatureLevel:
	// < 3: Chunks, < 6: ChunksV2, < 15: ChunksV3, >= 15: ChunksV4
	chunksSubdir := "ChunksV4"
	if featureLevel < 3 {
		chunksSubdir = "Chunks"
	} else if featureLevel < 6 {
		chunksSubdir = "ChunksV2"
	} else if featureLevel < 15 {
		chunksSubdir = "ChunksV3"
	} else {
		chunksSubdir = "ChunksV4"
	}

	// 2. Section 2: ChunkDataList begins at exactly metaSize
	chunkSectionStart := metaSize
	if chunkSectionStart+9 > len(payload) {
		return nil, fmt.Errorf("unexpected EOF reading chunk data section header (offset %d, payload %d)", chunkSectionStart, len(payload))
	}

	chunkSectionSize := int(binary.LittleEndian.Uint32(payload[chunkSectionStart : chunkSectionStart+4]))
	if chunkSectionSize < 9 || chunkSectionStart+chunkSectionSize > len(payload) {
		return nil, fmt.Errorf("invalid chunk section size %d at offset %d (payload %d)", chunkSectionSize, chunkSectionStart, len(payload))
	}

	// payload[chunkSectionStart+4] is dataVersion (EChunkDataListVersion)
	// payload[chunkSectionStart+5 : chunkSectionStart+9] is elementCount (int32)
	chunkCount := int(binary.LittleEndian.Uint32(payload[chunkSectionStart+5 : chunkSectionStart+9]))
	dataOffset := chunkSectionStart + 9

	if chunkCount <= 0 {
		return &ParsedManifest{
			FeatureLevel: featureLevel,
			ChunksSubdir: chunksSubdir,
			Chunks:       []EpicChunk{},
		}, nil
	}

	// Struct of Arrays (SoA):
	// GUIDs:                chunkCount * 16 bytes (4x uint32 LE)
	// Hashes:               chunkCount * 8 bytes (uint64 LE)
	// ShaHashes:            chunkCount * 20 bytes ([20]byte)
	// GroupNumbers:         chunkCount * 1 byte (uint8)
	// DataSizeUncompressed: chunkCount * 4 bytes (uint32 LE)
	// FileSizes:            chunkCount * 8 bytes (int64 LE)
	guidOffset := dataOffset
	hashOffset := guidOffset + (chunkCount * 16)
	shaOffset := hashOffset + (chunkCount * 8)
	groupOffset := shaOffset + (chunkCount * 20)
	uncompressedOffset := groupOffset + (chunkCount * 1)
	fileSizeOffset := uncompressedOffset + (chunkCount * 4)
	endOffset := fileSizeOffset + (chunkCount * 8)

	if endOffset > chunkSectionStart+chunkSectionSize || endOffset > len(payload) {
		return nil, fmt.Errorf("chunk data arrays exceed section length (needed %d, section has %d, payload %d, chunkCount %d)",
			endOffset, chunkSectionStart+chunkSectionSize, len(payload), chunkCount)
	}

	chunks := make([]EpicChunk, chunkCount)
	var totalSize int64

	for i := 0; i < chunkCount; i++ {
		// Read 16-byte GUID (4x uint32 little endian: A, B, C, D)
		gBase := guidOffset + (i * 16)
		g0 := binary.LittleEndian.Uint32(payload[gBase : gBase+4])
		g1 := binary.LittleEndian.Uint32(payload[gBase+4 : gBase+8])
		g2 := binary.LittleEndian.Uint32(payload[gBase+8 : gBase+12])
		g3 := binary.LittleEndian.Uint32(payload[gBase+12 : gBase+16])
		guidStr := fmt.Sprintf("%08X%08X%08X%08X", g0, g1, g2, g3)

		// Read 8-byte Hash (%016X)
		hBase := hashOffset + (i * 8)
		hVal := binary.LittleEndian.Uint64(payload[hBase : hBase+8])
		hashStr := fmt.Sprintf("%016X", hVal)

		// Read 1-byte GroupNumber (%02d)
		groupNum := payload[groupOffset+i]
		groupStr := fmt.Sprintf("%02d", groupNum)

		// Read 8-byte FileSize (int64)
		fBase := fileSizeOffset + (i * 8)
		fSize := int64(binary.LittleEndian.Uint64(payload[fBase : fBase+8]))
		if fSize <= 0 {
			uSize := int64(binary.LittleEndian.Uint32(payload[uncompressedOffset+(i*4) : uncompressedOffset+(i*4)+4]))
			if uSize > 0 {
				fSize = uSize
			} else {
				fSize = 1048576 // 1 MB default
			}
		}
		totalSize += fSize

		chunkID := fmt.Sprintf("%s_%s", hashStr, guidStr)
		relPath := fmt.Sprintf("%s/%s/%s_%s.chunk", chunksSubdir, groupStr, hashStr, guidStr)

		chunks[i] = EpicChunk{
			ChunkID:      chunkID,
			GUID:         guidStr,
			Hash:         hashStr,
			GroupNumber:  groupStr,
			RelativePath: relPath,
			SizeBytes:    fSize,
		}
	}

	return &ParsedManifest{
		FeatureLevel: featureLevel,
		ChunksSubdir: chunksSubdir,
		Chunks:       chunks,
		TotalSize:    totalSize,
	}, nil
}

// decodeBlobString decodes an Epic 3-digit decimal byte group string in little-endian order.
func decodeBlobString(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s)%3 != 0 {
		return 0, fmt.Errorf("invalid blob string length %d", len(s))
	}
	numBytes := len(s) / 3
	var raw uint64
	for i := 0; i < numBytes && i < 8; i++ {
		h := s[i*3 : i*3+3]
		v := int(h[0]-'0')*100 + int(h[1]-'0')*10 + int(h[2]-'0')
		if v > 255 || v < 0 {
			return 0, fmt.Errorf("invalid byte group %s", h)
		}
		raw |= uint64(v) << (i * 8)
	}
	return raw, nil
}

func parseChunkSize(val interface{}) int64 {
	switch v := val.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	case string:
		v = strings.TrimSpace(v)
		// 1. Try 3-digit decimal byte groups (e.g. 24, 18, 12, or 6 digits)
		if len(v) > 0 && len(v)%3 == 0 {
			isDigits := true
			for _, r := range v {
				if r < '0' || r > '9' {
					isDigits = false
					break
				}
			}
			if isDigits {
				if decoded, err := decodeBlobString(v); err == nil && decoded > 0 {
					return int64(decoded)
				}
			}
		}
		// 2. Try hex (e.g. "0b80010000000000" or "0x100000")
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			if n, err := strconv.ParseInt(v[2:], 16, 64); err == nil {
				return n
			}
		}
		if len(v) == 16 {
			if b, err := hex.DecodeString(v); err == nil && len(b) == 8 {
				return int64(binary.LittleEndian.Uint64(b))
			}
		}
		// 3. Try standard decimal string
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 1048576 // 1MB default
}

func parseChunkHash(val interface{}) string {
	switch v := val.(type) {
	case float64:
		return fmt.Sprintf("%016X", uint64(v))
	case uint64:
		return fmt.Sprintf("%016X", v)
	case int64:
		return fmt.Sprintf("%016X", uint64(v))
	case json.Number:
		if n, err := strconv.ParseUint(v.String(), 10, 64); err == nil {
			return fmt.Sprintf("%016X", n)
		}
	case string:
		v = strings.TrimSpace(v)
		// 1. 24-character blob string (8 bytes * 3 digits)
		if len(v) == 24 {
			isDigits := true
			for _, r := range v {
				if r < '0' || r > '9' {
					isDigits = false
					break
				}
			}
			if isDigits {
				if decoded, err := decodeBlobString(v); err == nil {
					return fmt.Sprintf("%016X", decoded)
				}
			}
		}
		// 2. 16 hex characters
		if len(v) == 16 {
			if _, err := hex.DecodeString(v); err == nil {
				return strings.ToUpper(v)
			}
		}
		// 3. Decimal string
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return fmt.Sprintf("%016X", n)
		}
		if len(v) > 0 {
			return strings.ToUpper(v)
		}
	}
	return "0000000000000000"
}

func parseDataGroup(val interface{}) string {
	switch v := val.(type) {
	case float64:
		return fmt.Sprintf("%02d", int(v))
	case int:
		return fmt.Sprintf("%02d", v)
	case json.Number:
		if n, err := strconv.Atoi(v.String()); err == nil {
			return fmt.Sprintf("%02d", n)
		}
	case string:
		v = strings.TrimSpace(v)
		// 3-digit decimal byte group (e.g. "042" or "000")
		if len(v) == 3 && v[0] >= '0' && v[0] <= '9' && v[1] >= '0' && v[1] <= '9' && v[2] >= '0' && v[2] <= '9' {
			num := int(v[0]-'0')*100 + int(v[1]-'0')*10 + int(v[2]-'0')
			return fmt.Sprintf("%02d", num)
		}
		if n, err := strconv.Atoi(v); err == nil {
			return fmt.Sprintf("%02d", n)
		}
		if len(v) > 0 {
			return v
		}
	}
	return "00"
}

// ParseJSONManifest parses legacy JSON Epic manifests
func ParseJSONManifest(data []byte) (*ParsedManifest, error) {
	type jsonFilePart struct {
		Guid string `json:"Guid"`
	}
	type jsonFileItem struct {
		Filename       string         `json:"Filename"`
		FileChunkParts []jsonFilePart `json:"FileChunkParts"`
	}

	var jsonManifest struct {
		ManifestFileVersion string                 `json:"ManifestFileVersion"`
		AppNameString       string                 `json:"AppNameString"`
		BuildVersionString  string                 `json:"BuildVersionString"`
		DataGroupList       map[string]interface{} `json:"DataGroupList"`
		ChunkHashList       map[string]interface{} `json:"ChunkHashList"`
		ChunkShaList        map[string]interface{} `json:"ChunkShaList"`
		ChunkFilesizeList   map[string]interface{} `json:"ChunkFilesizeList"`
		FileManifestList    []jsonFileItem         `json:"FileManifestList"`
	}

	if err := json.Unmarshal(data, &jsonManifest); err != nil {
		return nil, fmt.Errorf("failed to parse JSON manifest: %w", err)
	}

	chunksSubdir := "ChunksV4"
	if jsonManifest.ManifestFileVersion != "" {
		ver := uint64(0)
		if v, err := decodeBlobString(jsonManifest.ManifestFileVersion); err == nil {
			ver = v
		} else if n, err := strconv.ParseUint(jsonManifest.ManifestFileVersion, 10, 64); err == nil {
			ver = n
		}
		// In Epic's schema, version 13 is FeatureLevelBrokenJSONVersion which actually maps to ChunksV4
		if ver > 0 && ver < 13 {
			chunksSubdir = "Chunks"
		} else {
			chunksSubdir = "ChunksV4"
		}
	}

	// Collect all unique GUIDs across ChunkHashList, ChunkFilesizeList, and FileManifestList
	seenGUIDs := make(map[string]bool)
	var orderedGUIDs []string
	addGUID := func(guid string) {
		clean := strings.ToUpper(strings.ReplaceAll(guid, "-", ""))
		if clean == "" || seenGUIDs[clean] {
			return
		}
		seenGUIDs[clean] = true
		orderedGUIDs = append(orderedGUIDs, guid)
	}

	for guid := range jsonManifest.ChunkHashList {
		addGUID(guid)
	}
	for guid := range jsonManifest.ChunkFilesizeList {
		addGUID(guid)
	}
	for _, file := range jsonManifest.FileManifestList {
		for _, part := range file.FileChunkParts {
			if part.Guid != "" {
				addGUID(part.Guid)
			}
		}
	}

	chunks := make([]EpicChunk, 0, len(orderedGUIDs))
	var totalSize int64

	for _, guid := range orderedGUIDs {
		cleanGUID := strings.ToUpper(strings.ReplaceAll(guid, "-", ""))
		hashStr := parseChunkHash(jsonManifest.ChunkHashList[guid])
		if hashStr == "0000000000000000" {
			sum := sha1.Sum([]byte(guid))
			hashStr = fmt.Sprintf("%016X", binary.LittleEndian.Uint64(sum[:8]))
		}
		group := parseDataGroup(jsonManifest.DataGroupList[guid])
		size := parseChunkSize(jsonManifest.ChunkFilesizeList[guid])
		if size <= 0 {
			size = 1048576
		}
		totalSize += size

		chunkID := fmt.Sprintf("%s_%s", hashStr, cleanGUID)
		relPath := fmt.Sprintf("%s/%s/%s.chunk", chunksSubdir, group, chunkID)

		chunks = append(chunks, EpicChunk{
			ChunkID:      chunkID,
			GUID:         cleanGUID,
			Hash:         hashStr,
			GroupNumber:  group,
			RelativePath: relPath,
			SizeBytes:    size,
		})
	}

	return &ParsedManifest{
		AppName:      jsonManifest.AppNameString,
		BuildVersion: jsonManifest.BuildVersionString,
		ChunksSubdir: chunksSubdir,
		Chunks:       chunks,
		TotalSize:    totalSize,
	}, nil
}

// ToChunkMeta converts Epic chunks into generic ports.ChunkMeta
func (m *ParsedManifest) ToChunkMeta() []ports.ChunkMeta {
	metas := make([]ports.ChunkMeta, len(m.Chunks))
	for i, c := range m.Chunks {
		metas[i] = ports.ChunkMeta{
			ChunkID:   c.ChunkID,
			SizeBytes: c.SizeBytes,
		}
	}
	return metas
}
