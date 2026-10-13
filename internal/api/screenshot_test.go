package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatMB(t *testing.T) {
	assert.Equal(t, "4.0 MB", FormatMB(4*1024*1024))
	assert.Equal(t, "4.8 MB", FormatMB(5033164)) // ~4.8 MB
}

func TestDetectImageContentType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0, 0}
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0, 0)

	assert.Equal(t, "image/png", DetectImageContentType(png))
	assert.Equal(t, "image/jpeg", DetectImageContentType(jpeg))
	assert.Equal(t, "image/webp", DetectImageContentType(webp))
	assert.Equal(t, "", DetectImageContentType([]byte("not an image")))
}

// TestUploadScreenshot_LocalChecks exercises the pre-request validation
// UploadScreenshot does before ever opening a connection - no mock server is
// needed since these never get that far.
func TestUploadScreenshot_LocalChecks(t *testing.T) {
	c := NewAuthedClient("tok")

	t.Run("wrong type names the file", func(t *testing.T) {
		_, err := c.UploadScreenshot("ABC123", "weird.txt", []byte("not an image"))
		require.ErrorContains(t, err, "weird.txt")
		require.ErrorContains(t, err, "PNG, JPEG or WebP")
	})

	t.Run("wrong type with no filename falls back to a generic phrase", func(t *testing.T) {
		_, err := c.UploadScreenshot("ABC123", "", []byte("not an image"))
		require.ErrorContains(t, err, "the screenshot")
	})

	t.Run("too large names the file and gives sizes in MB", func(t *testing.T) {
		data := make([]byte, MaxScreenshotBytes+1)
		copy(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
		_, err := c.UploadScreenshot("ABC123", "big.png", data)
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "big.png is "), "got: %s", err.Error())
		assert.Contains(t, err.Error(), "MB")
		assert.Contains(t, err.Error(), "screenshots can be at most")
	})
}
