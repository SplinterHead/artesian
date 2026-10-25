package xbox

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHashProductID(t *testing.T) {
	id1 := "9NBLGGH4AB12"
	id2 := "9nblggh4ab12"
	h1 := HashProductID(id1)
	h2 := HashProductID(id2)

	if h1 <= 0 || h1 > 9007199254740991 {
		t.Fatalf("expected positive int64 hash within JS safe integer limits (<= 9007199254740991), got %d", h1)
	}
	if h1 != h2 {
		t.Fatalf("expected case-insensitive hash equality: %d != %d", h1, h2)
	}

	diffID := "9P9WXZH2L50M"
	if HashProductID(diffID) == h1 {
		t.Fatalf("expected distinct hash for different product IDs")
	}
}

func TestNetworkPort_MatchesHost(t *testing.T) {
	adapter := NewAdapter(nil, nil, nil)

	testHosts := []struct {
		host     string
		expected bool
	}{
		{"assets1.xboxlive.com", true},
		{"assets1.xboxlive.com:80", true},
		{"xvcf1.xboxlive.com", true},
		{"sub.assets1.xboxlive.com", true},
		{"tlu.dl.delivery.mp.microsoft.com", true},
		{"dl.delivery.mp.microsoft.com:80", true},
		{"steamcontent.com", false},
		{"google.com", false},
	}

	for _, tc := range testHosts {
		matched := adapter.MatchesHost(tc.host)
		if matched != tc.expected {
			t.Errorf("MatchesHost(%q) = %v, expected %v", tc.host, matched, tc.expected)
		}
	}
}

func TestNetworkPort_IsCacheable(t *testing.T) {
	adapter := NewAdapter(nil, nil, nil)

	testReqs := []struct {
		method   string
		path     string
		expected bool
	}{
		{"GET", "/filestream/abc123-guid.msixvc", true},
		{"HEAD", "/filestream/abc123-guid.msixvc", true},
		{"GET", "/filestream/abc123-guid.phf", true},
		{"GET", "/filestream/abc123-guid.ecd", true},
		{"GET", "/tlu/abc/package.appx", true},
		{"POST", "/filestream/abc123-guid.msixvc", false},
		{"GET", "/api/v1/user/profile", false},
	}

	for _, tc := range testReqs {
		req, _ := http.NewRequest(tc.method, "http://assets1.xboxlive.com"+tc.path, nil)
		cacheable := adapter.IsCacheable(req)
		if cacheable != tc.expected {
			t.Errorf("IsCacheable(%s %s) = %v, expected %v", tc.method, tc.path, cacheable, tc.expected)
		}
	}
}

func TestContentPort_BuildChunkURL(t *testing.T) {
	adapter := NewAdapter(nil, nil, nil)

	// Sliced chunk key
	chunkKey := "/filestream/testgame.msixvc@range=0-1048575"
	url := adapter.BuildChunkURL("assets1.xboxlive.com", 12345, chunkKey)
	expected := "http://assets1.xboxlive.com/filestream/testgame.msixvc"
	if url != expected {
		t.Fatalf("BuildChunkURL() = %q, expected %q", url, expected)
	}

	// Full .phf file
	phfKey := "/filestream/testgame.phf"
	urlPhf := adapter.BuildChunkURL("assets1.xboxlive.com", 12345, phfKey)
	expectedPhf := "http://assets1.xboxlive.com/filestream/testgame.phf"
	if urlPhf != expectedPhf {
		t.Fatalf("BuildChunkURL() = %q, expected %q", urlPhf, expectedPhf)
	}
}

func TestContentPort_ResolveManifestChunks(t *testing.T) {
	adapter := NewAdapter(nil, nil, nil)

	// Inject a pre-cached package
	appID := int64(987654321)
	adapter.packageMap[appID] = &CachedPackage{
		ProductID:            "9NBLGGH4TEST",
		ContentID:            "guid-test-1234",
		Title:                "Halo Infinite",
		Platforms:            []string{"Windows"},
		CdnRoots:             []string{"http://assets1.xboxlive.com"},
		BasePackageURL:       "/filestream/halo.msixvc",
		PieceHashFileURL:     "/filestream/halo.phf",
		ContentDescriptorURL: "/filestream/halo.ecd",
		PackageSize:          3145728, // 3 MB (exactly 3 x 1MB slices)
		PhfSize:              2048,
		EcdSize:              512,
	}

	chunks, err := adapter.ResolveManifestChunks(context.Background(), appID, appID, 1)
	if err != nil {
		t.Fatalf("ResolveManifestChunks returned error: %v", err)
	}

	// Should have: 1 ecd chunk + 1 phf chunk + 3 msixvc 1MB slice chunks = 5 total
	if len(chunks) != 5 {
		t.Fatalf("expected 5 chunks (ecd + phf + 3 slices), got %d", len(chunks))
	}

	if chunks[0].ChunkID != "/filestream/halo.ecd" || chunks[0].SizeBytes != 512 {
		t.Errorf("unexpected chunk 0: %+v", chunks[0])
	}
	if chunks[1].ChunkID != "/filestream/halo.phf" || chunks[1].SizeBytes != 2048 {
		t.Errorf("unexpected chunk 1: %+v", chunks[1])
	}

	// Slice 0: 0-1048575
	if chunks[2].ChunkID != "/filestream/halo.msixvc@range=0-1048575" || chunks[2].SizeBytes != 1048576 {
		t.Errorf("unexpected slice 0: %+v", chunks[2])
	}
	// Slice 1: 1048576-2097151
	if chunks[3].ChunkID != "/filestream/halo.msixvc@range=1048576-2097151" || chunks[3].SizeBytes != 1048576 {
		t.Errorf("unexpected slice 1: %+v", chunks[3])
	}
	// Slice 2: 2097152-3145727
	if chunks[4].ChunkID != "/filestream/halo.msixvc@range=2097152-3145727" || chunks[4].SizeBytes != 1048576 {
		t.Errorf("unexpected slice 2: %+v", chunks[4])
	}
}

func TestContentPort_ParseChunkKey(t *testing.T) {
	adapter := NewAdapter(nil, nil, nil)

	key := "/filestream/abc.msixvc@range=0-1048575?P1=123&P2=456"
	depotID, chunkHash, ok := adapter.ParseChunkKey(key)
	if !ok {
		t.Fatalf("expected ParseChunkKey to return ok=true")
	}
	if strings.Contains(chunkHash, "?") {
		t.Fatalf("expected query string to be stripped, got %q", chunkHash)
	}
	if depotID <= 0 {
		t.Fatalf("expected positive depotID, got %d", depotID)
	}
}
