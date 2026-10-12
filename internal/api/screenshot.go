// screenshot.go implements PUT /api/drafts/{code}/screenshot: magic-byte
// content-type detection plus the multipart upload itself, on its own
// (longer) timeout.
package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

// MaxScreenshotBytes is the contract's screenshot size cap (4 MB - Vercel's
// own body limit is 4.5 MB, so the hub refuses anything bigger before ever
// reaching that ceiling). Exported so cmd/publish.go's local pre-check uses
// the exact same number instead of a second hard-coded constant that could
// drift from this one.
const MaxScreenshotBytes = 4 * 1024 * 1024

// screenshotUploadTimeout bounds the screenshot PUT specifically. It's
// longer than the client's normal 30s timeout since a multipart upload of up
// to 4MB over a slow connection can reasonably take longer than any other
// request this CLI makes.
const screenshotUploadTimeout = 2 * time.Minute

// DetectImageContentType sniffs data's magic bytes and returns the MIME type
// stellar-hub accepts for a screenshot (image/png, image/jpeg, image/webp),
// or "" if none match. Used both by cmd/publish.go's local pre-check and by
// UploadScreenshot itself, so the two can never disagree on what counts as
// a valid image.
func DetectImageContentType(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

// extForContentType returns a plausible filename extension for the
// multipart part's Content-Disposition - cosmetic only, since the hub
// decides the actual type from the part's Content-Type header, not the
// filename.
func extForContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// UploadScreenshot calls PUT /api/drafts/{code}/screenshot with data as a
// multipart "screenshot" part, its Content-Type set from data's magic bytes
// (via CreatePart, not CreateFormFile, which would default the part to
// application/octet-stream). Returns a local error without making a request
// at all if the bytes don't look like a PNG/JPEG/WebP - the hub would reject
// it anyway, and this avoids uploading up to 4MB just to find that out.
func (c *Client) UploadScreenshot(code string, data []byte) (*Draft, error) {
	contentType := DetectImageContentType(data)
	if contentType == "" {
		return nil, fmt.Errorf("not a PNG, JPEG or WebP image")
	}
	if len(data) > MaxScreenshotBytes {
		return nil, fmt.Errorf("screenshot too large (max %d bytes)", MaxScreenshotBytes)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="screenshot"; filename="screenshot%s"`, extForContentType(contentType)))
	header.Set("Content-Type", contentType)

	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPut, c.baseURL+"/api/drafts/"+code+"/screenshot", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	uploadClient := &http.Client{
		Transport:     c.httpClient.Transport,
		CheckRedirect: c.httpClient.CheckRedirect,
		Timeout:       screenshotUploadTimeout,
	}

	resp, err := uploadClient.Do(req)
	if err != nil {
		return nil, wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.classifyNewEndpointError(resp)
	}
	return c.decodeDraft(resp)
}
