package epic

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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

// ParseJSONManifest parses legacy JSON Epic manifests
func ParseJSONManifest(data []byte) (*ParsedManifest, error) {
	var jsonManifest struct {
		AppNameString      string            `json:"AppNameString"`
		BuildVersionString string            `json:"BuildVersionString"`
		DataGroupList      map[string]string `json:"DataGroupList"`
		ChunkHashList      map[string]string `json:"ChunkHashList"`
		ChunkFilesizeList  map[string]int64  `json:"ChunkFilesizeList"`
	}

	if err := json.Unmarshal(data, &jsonManifest); err != nil {
		return nil, fmt.Errorf("failed to parse JSON manifest: %w", err)
	}

	chunks := make([]EpicChunk, 0, len(jsonManifest.ChunkHashList))
	var totalSize int64

	for guid, hash := range jsonManifest.ChunkHashList {
		group := jsonManifest.DataGroupList[guid]
		if group == "" {
			group = "00"
		}
		size := jsonManifest.ChunkFilesizeList[guid]
		if size <= 0 {
			size = 1048576
		}
		totalSize += size

		cleanGUID := strings.ToUpper(strings.ReplaceAll(guid, "-", ""))
		cleanHash := strings.ToUpper(hash)
		chunkID := fmt.Sprintf("%s_%s", cleanHash, cleanGUID)
		relPath := fmt.Sprintf("ChunksV4/%s/%s.chunk", group, chunkID)

		chunks = append(chunks, EpicChunk{
			ChunkID:      chunkID,
			GUID:         cleanGUID,
			Hash:         cleanHash,
			GroupNumber:  group,
			RelativePath: relPath,
			SizeBytes:    size,
		})
	}

	return &ParsedManifest{
		AppName:      jsonManifest.AppNameString,
		BuildVersion: jsonManifest.BuildVersionString,
		ChunksSubdir: "ChunksV4",
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
