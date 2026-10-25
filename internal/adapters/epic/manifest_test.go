package epic

import (
	"testing"
)

func TestParseJSONManifestBlobEncoding(t *testing.T) {
	// Sample JSON manifest with blob-encoded strings (as returned by Epic for certain titles)
	rawJSON := `{
		"ManifestFileVersion": "013000000000",
		"AppNameString": "GloveSkinApp",
		"BuildVersionString": "1.0.0",
		"DataGroupList": {
			"0123456789ABCDEF0123456789ABCDEF": "042"
		},
		"ChunkHashList": {
			"0123456789ABCDEF0123456789ABCDEF": "001002003004005006007008"
		},
		"ChunkFilesizeList": {
			"0123456789ABCDEF0123456789ABCDEF": "000000016000000000000000"
		}
	}`

	parsed, err := ParseJSONManifest([]byte(rawJSON))
	if err != nil {
		t.Fatalf("failed to parse JSON manifest with blob strings: %v", err)
	}

	if parsed.AppName != "GloveSkinApp" {
		t.Errorf("expected AppName GloveSkinApp, got %s", parsed.AppName)
	}
	if parsed.ChunksSubdir != "ChunksV4" {
		t.Errorf("expected ChunksSubdir ChunksV4 (version 13 maps to ChunksV4), got %s", parsed.ChunksSubdir)
	}
	if len(parsed.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(parsed.Chunks))
	}

	c := parsed.Chunks[0]
	if c.GroupNumber != "42" {
		t.Errorf("expected group 42, got %s", c.GroupNumber)
	}
	// 000 000 016 000 000 000 000 000 = 16 << 16 = 1048576 bytes
	if c.SizeBytes != 1048576 {
		t.Errorf("expected size 1048576, got %d", c.SizeBytes)
	}
	if parsed.TotalSize != 1048576 {
		t.Errorf("expected total size 1048576, got %d", parsed.TotalSize)
	}
}

func TestParseJSONManifestStandardNumbers(t *testing.T) {
	rawJSON := `{
		"AppNameString": "StandardApp",
		"BuildVersionString": "2.0.0",
		"DataGroupList": {
			"ABCDEF0123456789ABCDEF0123456789": "01"
		},
		"ChunkHashList": {
			"ABCDEF0123456789ABCDEF0123456789": "1234567890ABCDEF"
		},
		"ChunkFilesizeList": {
			"ABCDEF0123456789ABCDEF0123456789": 524288
		}
	}`

	parsed, err := ParseJSONManifest([]byte(rawJSON))
	if err != nil {
		t.Fatalf("failed to parse standard JSON manifest: %v", err)
	}

	if len(parsed.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(parsed.Chunks))
	}
	c := parsed.Chunks[0]
	if c.SizeBytes != 524288 {
		t.Errorf("expected size 524288, got %d", c.SizeBytes)
	}
	if c.Hash != "1234567890ABCDEF" {
		t.Errorf("expected hash 1234567890ABCDEF, got %s", c.Hash)
	}
}
