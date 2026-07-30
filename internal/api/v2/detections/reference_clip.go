package detections

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/audiocore/ffmpeg"
	"github.com/tphakala/birdnet-go/internal/branding"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/httpclient"
	"github.com/tphakala/birdnet-go/internal/logger"
	"github.com/tphakala/birdnet-go/internal/reference"
)

// Trusted-example clip processing tuning. The endpoint fetches the server-derived
// example audio, crops the first few seconds and loudness-normalizes it so the
// "Compare sounds" A/B screen plays a short example at a consistent, audible
// level regardless of how quiet or loud the original catalog recording is.
const (
	// referenceClipCropSeconds is how many seconds from the start of the example
	// are kept. A few seconds is enough to recognise a call while keeping the
	// back-to-back comparison quick.
	referenceClipCropSeconds = 6

	// referenceAudioHTTPTimeout bounds the outbound fetch of the example audio.
	referenceAudioHTTPTimeout = 20 * time.Second

	// maxReferenceAudioBytes caps how many bytes of example audio are downloaded
	// before processing, so a hostile or mislabelled source cannot exhaust
	// memory/disk.
	maxReferenceAudioBytes = 30 << 20 // 30 MiB

	// maxReferenceRedirects caps redirect hops while fetching example audio.
	maxReferenceRedirects = 5

	// referenceClipContentType is the MIME type of the processed clip.
	referenceClipContentType = "audio/mpeg"

	// referenceClipCacheMaxEntries bounds the in-memory processed-clip cache.
	// Clips are a few seconds of MP3 (tens of KB), so this caps memory at a few
	// MiB while still avoiding repeated downloads and ffmpeg passes.
	referenceClipCacheMaxEntries = 64

	// referenceClipBrowserCacheSeconds is the Cache-Control max-age advertised for
	// processed clips (they are keyed by immutable catalog recordings).
	referenceClipBrowserCacheSeconds = 86400 // 24h
)

// allowedReferenceAudioHost is the only host (plus its subdomains) the server
// will fetch trusted-example audio from. The URL is server-derived from the
// Xeno-canto search for the detection's species, never user-supplied; this
// allowlist is defense-in-depth (an SSRF guard) against the provider returning —
// or being coerced into returning — an off-site or internal URL.
const allowedReferenceAudioHost = "xeno-canto.org"

// isAllowedReferenceAudioURL reports whether rawURL is an https URL whose host is
// xeno-canto.org or a subdomain of it and is not an IP literal. It is the SSRF
// guard for outbound trusted-example audio fetches, kept as a pure function so it
// can be unit-tested exhaustively without network access.
func isAllowedReferenceAudioURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return false
	}
	// Reject IP literals outright — the provider serves audio from named hosts,
	// and a bare IP is a classic SSRF vector (link-local, loopback, metadata).
	if net.ParseIP(host) != nil {
		return false
	}
	return host == allowedReferenceAudioHost || strings.HasSuffix(host, "."+allowedReferenceAudioHost)
}

// referenceClipCacheKey derives a stable cache key for a processed clip from the
// provider, the (validated, server-derived) source audio URL and the crop length.
func referenceClipCacheKey(provider, audioURL string, cropSeconds int) string {
	return fmt.Sprintf("%s|%d|%s", provider, cropSeconds, audioURL)
}

// referenceClipCache is a small bounded in-memory cache of processed example
// clips keyed by referenceClipCacheKey. Eviction is FIFO on insertion order; the
// cached byte slices are treated as immutable and only ever read.
type referenceClipCache struct {
	mu         sync.Mutex
	entries    map[string][]byte
	order      []string
	maxEntries int
}

// newReferenceClipCache constructs a cache holding at most maxEntries entries.
func newReferenceClipCache(maxEntries int) *referenceClipCache {
	return &referenceClipCache{entries: make(map[string][]byte), maxEntries: maxEntries}
}

// get returns the cached clip for key, if present.
func (c *referenceClipCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.entries[key]
	return data, ok
}

// put stores data under key, evicting the oldest entry when the cache is full.
func (c *referenceClipCache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		c.entries[key] = data
		return
	}
	if c.maxEntries > 0 {
		for len(c.order) >= c.maxEntries {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = data
	c.order = append(c.order, key)
}

// clipCache returns the lazily-initialized processed-clip cache.
func (c *Handler) clipCache() *referenceClipCache {
	c.refClipOnce.Do(func() {
		c.refClipCache = newReferenceClipCache(referenceClipCacheMaxEntries)
	})
	return c.refClipCache
}

// GetDetectionReferenceClip serves a cropped, loudness-normalized copy of the
// trusted-example recording for a detection's species so the "Compare sounds"
// screen can play a short example at a consistent level. The source URL is
// derived server-side from the reference search (never taken from the client),
// host-locked to Xeno-canto, size- and time-bounded, and processed with the same
// hardened ffmpeg pipeline used for detection clips.
//
// It is strictly best-effort: whenever the feature is disabled/unconfigured, the
// species has no usable example, ffmpeg is unavailable, or the fetch/processing
// fails, it returns a non-2xx status so the frontend quietly falls back to
// playing the raw catalog URL. It never blocks local detection playback.
func (c *Handler) GetDetectionReferenceClip(ctx echo.Context) error {
	settings := c.CurrentSettings()
	if !referenceEnabled(settings) {
		return c.HandleError(ctx, nil, "Reference recordings are not enabled", http.StatusNotFound)
	}
	if settings.Realtime.Audio.FfmpegPath == "" {
		// No ffmpeg means we cannot crop/normalize; let the client use the raw URL.
		return c.HandleError(ctx, nil, "Audio processing unavailable", http.StatusNotFound)
	}

	id := ctx.Param("id")
	note, err := c.DS.Get(id)
	if err != nil {
		return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	}

	client := c.referenceClient(settings.Realtime.IdentificationCheck.Xenocanto.APIKey)
	if !client.Configured() {
		return c.HandleError(ctx, nil, "Reference recordings are not configured", http.StatusNotFound)
	}

	recordings, err := client.Search(ctx.Request().Context(), note.ScientificName)
	if err != nil {
		// Best-effort: an online-source hiccup is warn-logged for operators but
		// returns a plain 404 (not a 5xx) so the client quietly falls back to the
		// raw URL without generating error telemetry for expected flakiness.
		c.LogWarnIfEnabled("reference clip lookup failed",
			logger.String("species", note.ScientificName),
			logger.Error(err))
		return c.HandleError(ctx, nil, "No reference example available", http.StatusNotFound)
	}

	ranked := reference.Rank(recordings, reference.Preferences{Month: monthFromDate(note.Date)})
	selected := selectReferenceRecording(ranked, ctx.QueryParam("rec"))
	if selected == nil {
		return c.HandleError(ctx, nil, "No reference example available", http.StatusNotFound)
	}

	if !isAllowedReferenceAudioURL(selected.AudioURL) {
		c.LogWarnIfEnabled("reference clip source host not allowed",
			logger.String("species", note.ScientificName))
		return c.HandleError(ctx, nil, "Reference example source not allowed", http.StatusNotFound)
	}

	key := referenceClipCacheKey(selected.SourceProvider, selected.AudioURL, referenceClipCropSeconds)
	if data, ok := c.clipCache().get(key); ok {
		return c.serveReferenceClip(ctx, data)
	}

	data, err := c.buildReferenceClip(ctx.Request().Context(), settings.Realtime.Audio.FfmpegPath, selected.AudioURL)
	if err != nil {
		// Same quiet-degradation contract as a lookup failure: warn-log and return
		// 404 so the client falls back to the raw catalog URL.
		c.LogWarnIfEnabled("reference clip processing failed",
			logger.String("species", note.ScientificName),
			logger.Error(err))
		return c.HandleError(ctx, nil, "Reference example could not be processed", http.StatusNotFound)
	}

	c.clipCache().put(key, data)
	return c.serveReferenceClip(ctx, data)
}

// selectReferenceRecording picks the ranked recording to process: the one whose
// catalog id matches recParam when given and found, otherwise the best-ranked.
// Returns nil when there are no usable recordings.
func selectReferenceRecording(ranked []reference.Recording, recParam string) *reference.Recording {
	if len(ranked) == 0 {
		return nil
	}
	if recParam != "" {
		for i := range ranked {
			if ranked[i].ID == recParam {
				return &ranked[i]
			}
		}
	}
	return &ranked[0]
}

// serveReferenceClip writes the processed clip bytes with an audio content type
// and a cache header.
func (c *Handler) serveReferenceClip(ctx echo.Context, data []byte) error {
	ctx.Response().Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", referenceClipBrowserCacheSeconds))
	return ctx.Blob(http.StatusOK, referenceClipContentType, data)
}

// buildReferenceClip downloads the (host-validated) example audio and returns a
// cropped, loudness-normalized MP3 rendered with the shared ffmpeg clip pipeline.
func (c *Handler) buildReferenceClip(ctx context.Context, ffmpegPath, audioURL string) ([]byte, error) {
	raw, err := c.downloadReferenceAudio(ctx, audioURL)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "birdnet-refclip-*")
	if err != nil {
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryFileIO).
			Context("operation", "reference_clip_tempfile").
			Build()
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryFileIO).
			Context("operation", "reference_clip_tempwrite").
			Build()
	}
	if err := tmp.Close(); err != nil {
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryFileIO).
			Context("operation", "reference_clip_tempclose").
			Build()
	}

	buf, err := ffmpeg.ExtractClip(ctx, &ffmpeg.ClipOptions{
		InputPath:  tmpPath,
		Start:      0,
		End:        float64(referenceClipCropSeconds),
		Format:     ffmpeg.FormatMP3,
		Filters:    &ffmpeg.AudioFilters{Normalize: true},
		FFmpegPath: ffmpegPath,
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downloadReferenceAudio fetches the example audio with a host-locked HTTP client
// (redirects may only stay on the allowed host), bounded in time and size.
func (c *Handler) downloadReferenceAudio(ctx context.Context, audioURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, http.NoBody)
	if err != nil {
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryNetwork).
			Context("operation", "reference_clip_request").
			Build()
	}
	req.Header.Set("User-Agent", fmt.Sprintf("BirdNET-Go (%s)", branding.RepoURL()))

	resp, err := c.referenceAudioDoer().Do(req)
	if err != nil {
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryNetwork).
			Context("operation", "reference_clip_download").
			Build()
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("reference audio source returned status %d", resp.StatusCode).
			Component("detections").
			Category(errors.CategoryHTTP).
			Context("status_code", resp.StatusCode).
			Build()
	}

	// Read one byte past the cap so an over-size body is detected rather than
	// silently truncated into a corrupt clip.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReferenceAudioBytes+1))
	if err != nil {
		return nil, errors.New(err).
			Component("detections").
			Category(errors.CategoryNetwork).
			Context("operation", "reference_clip_read").
			Build()
	}
	if len(data) > maxReferenceAudioBytes {
		return nil, errors.Newf("reference audio exceeds %d bytes", maxReferenceAudioBytes).
			Component("detections").
			Category(errors.CategoryValidation).
			Context("operation", "reference_clip_read").
			Build()
	}
	if len(data) == 0 {
		return nil, errors.Newf("reference audio source returned empty body").
			Component("detections").
			Category(errors.CategoryNetwork).
			Context("operation", "reference_clip_read").
			Build()
	}
	return data, nil
}

// referenceAudioDoer returns the injected test doer when set, otherwise a
// timeout-bounded HTTP client whose redirects are locked to the allowed host.
func (c *Handler) referenceAudioDoer() reference.Doer {
	if c.refClipDoer != nil {
		return c.refClipDoer
	}
	return &http.Client{
		Timeout:       referenceAudioHTTPTimeout,
		Transport:     httpclient.CloneDefaultTransport(),
		CheckRedirect: referenceAudioCheckRedirect,
	}
}

// referenceAudioCheckRedirect keeps trusted-example audio redirects on the
// allowed host and bounds the hop count, so a redirect cannot be used to reach an
// off-site or internal target.
func referenceAudioCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxReferenceRedirects {
		return errors.Newf("too many redirects fetching reference audio").
			Component("detections").
			Category(errors.CategoryNetwork).
			Build()
	}
	if !isAllowedReferenceAudioURL(req.URL.String()) {
		return errors.Newf("reference audio redirect to disallowed host").
			Component("detections").
			Category(errors.CategoryValidation).
			Build()
	}
	return nil
}
