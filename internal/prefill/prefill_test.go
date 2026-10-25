package prefill

import (
	"reflect"
	"testing"
)

func TestEpicAlternativeURLs(t *testing.T) {
	origURL := "https://egs-cloudfront-chunks.epicgamescdn.com/Builds/Org/app/ChunksV4/07/hash_guid.chunk"
	expected := []string{
		"https://egs-cloudfront-chunks.epicgamescdn.com/Builds/Org/app/Chunks/07/hash_guid.chunk",
		"https://egs-cloudfront-chunks.epicgamescdn.com/Builds/Org/app/ChunksV3/07/hash_guid.chunk",
		"https://egs-cloudfront-chunks.epicgamescdn.com/Builds/Org/app/ChunksV2/07/hash_guid.chunk",
	}

	alts := epicAlternativeURLs(origURL)
	if !reflect.DeepEqual(alts, expected) {
		t.Fatalf("expected %v, got %v", expected, alts)
	}

	// Test URL without any matching chunk directory
	noMatch := epicAlternativeURLs("https://example.com/other/path/file.chunk")
	if noMatch != nil {
		t.Fatalf("expected nil for non-matching URL, got %v", noMatch)
	}
}
