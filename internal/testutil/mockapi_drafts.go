// mockapi_drafts.go implements the mock's draft routes
// (/api/drafts, /api/drafts/{code}, .../screenshot, .../publish) from the
// API contract's "Drafts" table. Every route here is bearer-only, matching
// how the CLI (the only client these tests exercise) always talks to them.
package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// mockDraft is the mock's internal draft record. Every optional contract
// field is a pointer, mirroring internal/api.Draft, so "omitted"/"null"
// round-trip the same way a real draft would.
type mockDraft struct {
	code  string
	kind  string // "theme" | "version"
	owner string // username

	// themeSlug is set for version drafts: the slug of the existing theme
	// (under owner) this draft is a new version of.
	themeSlug string

	name, slug, description, group, colorMode, colorSchemeID *string
	dependencies                                             []string
	minStarshipVersion                                       *string
	version, versionNotes                                    *string
	config                                                   *string
	configSource                                             *string
	configUpdatedAt                                          *time.Time
	screenshotURL                                            *string
	source                                                   *string

	createdAt, updatedAt time.Time
}

// draftCodeAlphabet matches the contract's code alphabet (excludes visually
// ambiguous characters: 0/O, 1/I).
const draftCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func (h *MockAPIHandler) nextDraftCode() string {
	h.draftSeq++
	n := h.draftSeq
	b := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = draftCodeAlphabet[n%len(draftCodeAlphabet)]
		n /= len(draftCodeAlphabet)
	}
	return string(b)
}

// createDraftBody is the union of every field POST /api/drafts or PATCH
// /api/drafts/{code} can carry - kind/themeSlug are ignored by the PATCH
// handler (immutable per the contract).
type createDraftBody struct {
	Kind      string  `json:"kind"`
	Slug      *string `json:"slug"`
	ThemeSlug *string `json:"themeSlug"`

	Name               *string   `json:"name"`
	Description        *string   `json:"description"`
	Group              *string   `json:"group"`
	ColorMode          *string   `json:"colorMode"`
	Dependencies       *[]string `json:"dependencies"`
	MinStarshipVersion *string   `json:"minStarshipVersion"`
	Version            *string   `json:"version"`
	VersionNotes       *string   `json:"versionNotes"`
	Config             *string   `json:"config"`
	Source             *string   `json:"source"`
}

func validateMockConfig(content string) (bool, string) {
	if len(content) < 1 || len(content) > 100000 {
		return false, "config must be 1-100000 characters"
	}
	var v map[string]interface{}
	if _, err := toml.Decode(content, &v); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefStrOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// normalizeConfigForCompare mirrors the contract's own comparison rule for
// "did the config actually change" (configSource/configUpdatedAt) and for
// unchangedFromLatest: CRLF -> LF, then trimmed.
func normalizeConfigForCompare(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bumpMinorVersion returns the default next version for a new version draft:
// (X+1).0 of latest, per the contract.
func bumpMinorVersion(v string) string {
	major, _, ok := parseMockVersion(v)
	if !ok {
		return "1.0"
	}
	return fmt.Sprintf("%d.0", major+1)
}

// compareMockVersions and parseMockVersion duplicate
// internal/theme.CompareSemver's numeric X.Y comparison rather than
// importing internal/theme: that package's own tests (package theme, not
// theme_test) import testutil, so testutil importing theme back would be an
// import cycle - the same reason MockPing above duplicates api.CLIPing
// instead of importing internal/api.
func compareMockVersions(a, b string) int {
	aMajor, aMinor, aOk := parseMockVersion(a)
	bMajor, bMinor, bOk := parseMockVersion(b)
	if !aOk && !bOk {
		return strings.Compare(a, b)
	}
	if !aOk {
		return -1
	}
	if !bOk {
		return 1
	}
	if aMajor != bMajor {
		return aMajor - bMajor
	}
	return aMinor - bMinor
}

func parseMockVersion(v string) (major, minor int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return maj, m, true
}

func (h *MockAPIHandler) missingFields(d *mockDraft) []string {
	if d.kind == "theme" {
		var m []string
		if d.name == nil || *d.name == "" {
			m = append(m, "name")
		}
		if d.config == nil || *d.config == "" {
			m = append(m, "config")
		}
		if d.screenshotURL == nil {
			m = append(m, "screenshot")
		}
		return m
	}
	var m []string
	if d.version == nil || *d.version == "" {
		m = append(m, "version")
	}
	return m
}

func (h *MockAPIHandler) draftStatus(d *mockDraft) string {
	if d.configSource == nil || *d.configSource == "latest" {
		return "waiting_for_config"
	}
	if len(h.missingFields(d)) > 0 {
		return "incomplete"
	}
	return "ready"
}

// unchangedFromLatest compares config and dependencies against the theme's
// latest published version. minStarshipVersion is deliberately not compared
// - MockTheme/MockVersion (shared with every other mock test in this
// package) has no field for it - which is a known simplification of the
// mock, not of the contract.
func (h *MockAPIHandler) unchangedFromLatest(d *mockDraft, th *MockTheme) bool {
	if len(th.Versions) == 0 || d.config == nil {
		return false
	}
	latest := th.Versions[0]
	if normalizeConfigForCompare(*d.config) != normalizeConfigForCompare(latest.ConfigContent) {
		return false
	}
	return stringSlicesEqual(d.dependencies, latest.Dependencies)
}

// draftShape builds the JSON shape for d exactly matching internal/api.Draft's
// field names.
func (h *MockAPIHandler) draftShape(d *mockDraft, includeConfig bool) map[string]interface{} {
	deps := d.dependencies
	if deps == nil {
		deps = []string{}
	}
	missing := h.missingFields(d)
	if missing == nil {
		missing = []string{}
	}

	shape := map[string]interface{}{
		"code":                d.code,
		"kind":                d.kind,
		"status":              h.draftStatus(d),
		"url":                 fmt.Sprintf("https://mock.stellar-hub.test/upload/%s", d.code),
		"theme":               nil,
		"name":                d.name,
		"slug":                d.slug,
		"description":         d.description,
		"group":               d.group,
		"colorMode":           d.colorMode,
		"colorSchemeId":       d.colorSchemeID,
		"dependencies":        deps,
		"minStarshipVersion":  d.minStarshipVersion,
		"version":             d.version,
		"versionNotes":        d.versionNotes,
		"hasConfig":           d.config != nil && *d.config != "",
		"configSource":        d.configSource,
		"configUpdatedAt":     nil,
		"unchangedFromLatest": false,
		"screenshotUrl":       d.screenshotURL,
		"source":              d.source,
		"missing":             missing,
		"createdAt":           d.createdAt.Format(time.RFC3339),
		"updatedAt":           d.updatedAt.Format(time.RFC3339),
		"expiresAt":           d.updatedAt.Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}
	if d.configUpdatedAt != nil {
		shape["configUpdatedAt"] = d.configUpdatedAt.Format(time.RFC3339)
	}
	if includeConfig && d.config != nil {
		shape["config"] = *d.config
	}
	if d.kind == "version" {
		if th, ok := h.themes[d.owner+"/"+d.themeSlug]; ok {
			latest := ""
			if len(th.Versions) > 0 {
				latest = th.Versions[0].Version
			}
			shape["theme"] = map[string]interface{}{
				"slug": d.themeSlug, "name": th.Name, "latestVersion": latest,
			}
			shape["unchangedFromLatest"] = h.unchangedFromLatest(d, th)
		}
	}
	return shape
}

func (h *MockAPIHandler) handleDrafts(w http.ResponseWriter, r *http.Request, rest []string) {
	username, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	switch len(rest) {
	case 0:
		switch r.Method {
		case http.MethodGet:
			h.handleListDrafts(w, username)
		case http.MethodPost:
			h.handleCreateDraft(w, r, username)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case 1:
		code := strings.ToUpper(rest[0])
		switch r.Method {
		case http.MethodGet:
			h.handleGetDraft(w, r, username, code)
		case http.MethodPatch:
			h.handlePatchDraft(w, r, username, code)
		case http.MethodDelete:
			h.handleDeleteDraft(w, username, code)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case 2:
		code := strings.ToUpper(rest[0])
		switch rest[1] {
		case "screenshot":
			switch r.Method {
			case http.MethodPut:
				h.handlePutScreenshot(w, r, username, code)
			case http.MethodDelete:
				h.handleDeleteScreenshot(w, username, code)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		case "publish":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.handlePublishDraft(w, username, code)
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *MockAPIHandler) handleListDrafts(w http.ResponseWriter, username string) {
	h.mu.Lock()
	var mine []*mockDraft
	for _, d := range h.drafts {
		if d.owner == username {
			mine = append(mine, d)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].updatedAt.After(mine[j].updatedAt) })
	shapes := make([]map[string]interface{}, len(mine))
	for i, d := range mine {
		shapes[i] = h.draftShape(d, false)
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"drafts": shapes})
}

func (h *MockAPIHandler) handleCreateDraft(w http.ResponseWriter, r *http.Request, username string) {
	var body createDraftBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid JSON body", nil, nil)
		return
	}
	if body.Config != nil && *body.Config != "" {
		if ok, msg := validateMockConfig(*body.Config); !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_config", msg, nil, nil)
			return
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	live := 0
	for _, d := range h.drafts {
		if d.owner == username {
			live++
		}
	}
	if live >= 10 {
		writeJSONError(w, http.StatusConflict, "too_many_drafts", "you already have 10 live drafts", nil, nil)
		return
	}

	now := time.Now().UTC()

	switch body.Kind {
	case "theme":
		slug := derefStr(body.Slug)
		if slug == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "slug is required", nil, nil)
			return
		}
		if _, exists := h.themes[username+"/"+slug]; exists {
			writeJSONError(w, http.StatusConflict, "duplicate_slug", "you already have a published theme with that slug", nil, nil)
			return
		}
		for _, d := range h.drafts {
			if d.owner == username && d.kind == "theme" && d.slug != nil && *d.slug == slug {
				writeJSONError(w, http.StatusConflict, "draft_exists", "you already have a draft with that slug", nil, map[string]string{"code": d.code})
				return
			}
		}

		code := h.nextDraftCode()
		d := &mockDraft{code: code, kind: "theme", owner: username, slug: &slug, createdAt: now, updatedAt: now, dependencies: []string{}}
		d.name = body.Name
		d.description = body.Description
		d.group = body.Group
		d.colorMode = body.ColorMode
		if body.Dependencies != nil {
			d.dependencies = *body.Dependencies
		}
		d.minStarshipVersion = body.MinStarshipVersion
		version := derefStrOr(body.Version, "1.0")
		d.version = &version
		d.versionNotes = body.VersionNotes
		d.source = body.Source
		applyMockConfig(d, body.Config, now)

		h.drafts[code] = d
		writeJSON(w, http.StatusCreated, h.draftShape(d, true))

	case "version":
		slug := derefStr(body.ThemeSlug)
		if slug == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "themeSlug is required", nil, nil)
			return
		}
		th, exists := h.themes[username+"/"+slug]
		if !exists {
			writeJSONError(w, http.StatusNotFound, "theme_not_found", "theme not found", nil, nil)
			return
		}
		for _, d := range h.drafts {
			if d.owner == username && d.kind == "version" && d.themeSlug == slug {
				writeJSONError(w, http.StatusConflict, "draft_exists", "you already have a draft for a new version of this theme", nil, map[string]string{"code": d.code})
				return
			}
		}

		var latest MockVersion
		if len(th.Versions) > 0 {
			latest = th.Versions[0]
		}

		code := h.nextDraftCode()
		d := &mockDraft{code: code, kind: "version", owner: username, themeSlug: slug, createdAt: now, updatedAt: now}

		nextVer := bumpMinorVersion(latest.Version)
		if body.Version != nil && *body.Version != "" {
			nextVer = *body.Version
		}
		d.version = &nextVer

		if body.Dependencies != nil {
			d.dependencies = *body.Dependencies
		} else {
			d.dependencies = append([]string(nil), latest.Dependencies...)
		}
		if d.dependencies == nil {
			d.dependencies = []string{}
		}
		if body.MinStarshipVersion != nil {
			d.minStarshipVersion = body.MinStarshipVersion
		} else {
			msv := "1.24.0"
			d.minStarshipVersion = &msv
		}
		d.versionNotes = body.VersionNotes
		d.source = body.Source

		if body.Config != nil && *body.Config != "" {
			applyMockConfig(d, body.Config, now)
		} else {
			cfg := latest.ConfigContent
			src := "latest"
			d.config = &cfg
			d.configSource = &src
		}

		h.drafts[code] = d
		writeJSON(w, http.StatusCreated, h.draftShape(d, true))

	default:
		writeJSONError(w, http.StatusBadRequest, "validation_failed", `kind must be "theme" or "version"`, nil, nil)
	}
}

// applyMockConfig sets d's config/configSource/configUpdatedAt - every
// bearer request the mock serves is "from the CLI" by construction.
func applyMockConfig(d *mockDraft, config *string, at time.Time) {
	if config == nil {
		return
	}
	cfg := *config
	src := "cli"
	d.config = &cfg
	d.configSource = &src
	t := at
	d.configUpdatedAt = &t
}

func (h *MockAPIHandler) handleGetDraft(w http.ResponseWriter, r *http.Request, username, code string) {
	includeConfig := r.URL.Query().Get("config") != "0"

	h.mu.Lock()
	d, ok := h.drafts[code]
	if !ok || d.owner != username {
		h.mu.Unlock()
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}
	shape := h.draftShape(d, includeConfig)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, shape)
}

func (h *MockAPIHandler) handlePatchDraft(w http.ResponseWriter, r *http.Request, username, code string) {
	var body createDraftBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid JSON body", nil, nil)
		return
	}
	if body.Config != nil && *body.Config != "" {
		if ok, msg := validateMockConfig(*body.Config); !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_config", msg, nil, nil)
			return
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	d, ok := h.drafts[code]
	if !ok || d.owner != username {
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}

	if body.Slug != nil && d.kind == "theme" && *body.Slug != derefStr(d.slug) {
		newSlug := *body.Slug
		if _, exists := h.themes[username+"/"+newSlug]; exists {
			writeJSONError(w, http.StatusConflict, "duplicate_slug", "you already have a published theme with that slug", nil, nil)
			return
		}
		for oc, od := range h.drafts {
			if oc != code && od.owner == username && od.kind == "theme" && od.slug != nil && *od.slug == newSlug {
				writeJSONError(w, http.StatusConflict, "draft_exists", "you already have a draft with that slug", nil, map[string]string{"code": od.code})
				return
			}
		}
		d.slug = &newSlug
	}

	if body.Name != nil {
		d.name = body.Name
	}
	if body.Description != nil {
		d.description = body.Description
	}
	if body.Group != nil {
		d.group = body.Group
	}
	if body.ColorMode != nil {
		d.colorMode = body.ColorMode
	}
	if body.Dependencies != nil {
		d.dependencies = *body.Dependencies
	}
	if body.MinStarshipVersion != nil {
		d.minStarshipVersion = body.MinStarshipVersion
	}
	if body.Version != nil {
		d.version = body.Version
	}
	if body.VersionNotes != nil {
		d.versionNotes = body.VersionNotes
	}
	if body.Source != nil {
		d.source = body.Source
	}

	if body.Config != nil && *body.Config != "" {
		oldNorm := ""
		if d.config != nil {
			oldNorm = normalizeConfigForCompare(*d.config)
		}
		if normalizeConfigForCompare(*body.Config) != oldNorm {
			applyMockConfig(d, body.Config, time.Now().UTC())
		}
	}

	d.updatedAt = time.Now().UTC()
	shape := h.draftShape(d, true)
	writeJSON(w, http.StatusOK, shape)
}

func (h *MockAPIHandler) handleDeleteDraft(w http.ResponseWriter, username, code string) {
	h.mu.Lock()
	d, ok := h.drafts[code]
	if !ok || d.owner != username {
		h.mu.Unlock()
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}
	delete(h.drafts, code)
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// mockMaxScreenshotBytes duplicates api.MaxScreenshotBytes for the same
// import-cycle reason as compareMockVersions above.
const mockMaxScreenshotBytes = 4 * 1024 * 1024

func mockDetectImageContentType(data []byte) string {
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

func (h *MockAPIHandler) handlePutScreenshot(w http.ResponseWriter, r *http.Request, username, code string) {
	h.mu.Lock()
	d, ok := h.drafts[code]
	ownerOK := ok && d.owner == username
	h.mu.Unlock()
	if !ownerOK {
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}
	if d.kind != "theme" {
		writeJSONError(w, http.StatusBadRequest, "not_applicable", "version drafts don't have a screenshot", nil, nil)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "expected multipart form data", nil, nil)
		return
	}

	var data []byte
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "malformed multipart body", nil, nil)
			return
		}
		if part.FormName() != "screenshot" {
			continue
		}
		buf, rerr := io.ReadAll(io.LimitReader(part, mockMaxScreenshotBytes+1))
		_ = part.Close()
		if rerr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "couldn't read screenshot", nil, nil)
			return
		}
		data = buf
	}
	if data == nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "missing screenshot part", nil, nil)
		return
	}
	if len(data) > mockMaxScreenshotBytes {
		writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "screenshot too large", nil, nil)
		return
	}
	if mockDetectImageContentType(data) == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_screenshot", "not a PNG, JPEG or WebP image", nil, nil)
		return
	}

	h.mu.Lock()
	url := fmt.Sprintf("https://mock.stellar-hub.test/screenshots/%s.webp", d.code)
	d.screenshotURL = &url
	d.updatedAt = time.Now().UTC()
	shape := h.draftShape(d, true)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, shape)
}

func (h *MockAPIHandler) handleDeleteScreenshot(w http.ResponseWriter, username, code string) {
	h.mu.Lock()
	d, ok := h.drafts[code]
	if !ok || d.owner != username {
		h.mu.Unlock()
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}
	d.screenshotURL = nil
	d.updatedAt = time.Now().UTC()
	shape := h.draftShape(d, true)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, shape)
}

func (h *MockAPIHandler) handlePublishDraft(w http.ResponseWriter, username, code string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	d, ok := h.drafts[code]
	if !ok || d.owner != username {
		writeJSONError(w, http.StatusNotFound, "draft_not_found", "draft not found", nil, nil)
		return
	}

	if missing := h.missingFields(d); len(missing) > 0 {
		writeJSONError(w, http.StatusBadRequest, "draft_incomplete", "draft is incomplete", missing, nil)
		return
	}
	if d.config == nil || *d.config == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_config", "missing config", nil, nil)
		return
	}
	if ok, msg := validateMockConfig(*d.config); !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_config", msg, nil, nil)
		return
	}

	switch d.kind {
	case "theme":
		slug := derefStr(d.slug)
		if _, exists := h.themes[username+"/"+slug]; exists {
			writeJSONError(w, http.StatusConflict, "duplicate_slug", "a theme with that slug already exists", nil, nil)
			return
		}

		version := derefStrOr(d.version, "1.0")
		newTheme := MockTheme{
			ID:          "theme-" + slug,
			Author:      username,
			Slug:        slug,
			Name:        derefStr(d.name),
			Description: derefStr(d.description),
			Group:       derefStr(d.group),
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
			Versions: []MockVersion{{
				Version:       version,
				ConfigContent: *d.config,
				VersionNotes:  derefStr(d.versionNotes),
				Dependencies:  d.dependencies,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			}},
		}
		h.themes[username+"/"+slug] = &newTheme
		delete(h.drafts, code)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"author": username, "slug": slug, "version": version,
			"url": fmt.Sprintf("https://mock.stellar-hub.test/%s/%s", username, slug),
		})

	case "version":
		th, exists := h.themes[username+"/"+d.themeSlug]
		if !exists {
			writeJSONError(w, http.StatusNotFound, "theme_not_found", "theme not found", nil, nil)
			return
		}
		newVersion := derefStr(d.version)
		latest := ""
		if len(th.Versions) > 0 {
			latest = th.Versions[0].Version
		}
		for _, v := range th.Versions {
			if v.Version == newVersion {
				writeJSONError(w, http.StatusConflict, "version_exists", "that version already exists", nil, nil)
				return
			}
		}
		if latest != "" && compareMockVersions(newVersion, latest) <= 0 {
			writeJSONError(w, http.StatusBadRequest, "version_not_newer", "version must be newer than the latest", nil, map[string]string{"latest": latest})
			return
		}

		th.Versions = append([]MockVersion{{
			Version:       newVersion,
			ConfigContent: *d.config,
			VersionNotes:  derefStr(d.versionNotes),
			Dependencies:  d.dependencies,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		}}, th.Versions...)
		delete(h.drafts, code)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"author": username, "slug": d.themeSlug, "version": newVersion,
			"url": fmt.Sprintf("https://mock.stellar-hub.test/%s/%s", username, d.themeSlug),
		})
	}
}

// --- test-only helpers (not part of any real stellar-hub behavior) ---------

// SeedThemeDraft creates a theme draft directly, bypassing the CLI, for
// tests that need to attach to a draft that already existed - as if started
// on the web - rather than one the CLI itself just created. Returns the
// draft's code.
func (h *MockAPIHandler) SeedThemeDraft(owner, slug string, name *string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	code := h.nextDraftCode()
	now := time.Now().UTC()
	h.drafts[code] = &mockDraft{
		code: code, kind: "theme", owner: owner, slug: &slug, name: name,
		createdAt: now, updatedAt: now, dependencies: []string{},
	}
	return code
}

// DraftCount returns how many live drafts username currently owns.
func (h *MockAPIHandler) DraftCount(username string) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	n := 0
	for _, d := range h.drafts {
		if d.owner == username {
			n++
		}
	}
	return n
}
