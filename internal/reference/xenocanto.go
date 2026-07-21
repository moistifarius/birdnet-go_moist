package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/tphakala/birdnet-go/internal/branding"
	"github.com/tphakala/birdnet-go/internal/errors"
)

// defaultBaseURL is the Xeno-canto API v3 recordings endpoint.
const defaultBaseURL = "https://xeno-canto.org/api/3/recordings"

// Tuning defaults. Xeno-canto asks callers to be courteous, so the client rate
// limits requests and caches results per species.
const (
	defaultCacheTTL     = 24 * time.Hour
	defaultRateInterval = time.Second
	defaultRateBurst    = 5
	maxResponseBytes    = 8 << 20 // 8 MiB cap on the JSON response body
)

// Doer performs an HTTP request. *http.Client satisfies it; tests inject a fake.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client queries Xeno-canto for reference recordings. It caches results per
// species and rate-limits outbound requests. Construct it with NewClient.
type Client struct {
	doer      Doer
	apiKey    string
	baseURL   string
	userAgent string
	limiter   *rate.Limiter
	cacheTTL  time.Duration
	clock     func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	recordings []Recording
	expiresAt  time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (used in tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithUserAgent overrides the outbound User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithCacheTTL overrides how long search results are cached.
func WithCacheTTL(d time.Duration) Option { return func(c *Client) { c.cacheTTL = d } }

// WithRateLimiter overrides the outbound rate limiter.
func WithRateLimiter(l *rate.Limiter) Option { return func(c *Client) { c.limiter = l } }

// WithClock overrides the time source (used in tests for cache expiry).
func WithClock(now func() time.Time) Option { return func(c *Client) { c.clock = now } }

// NewClient constructs a Xeno-canto client. apiKey may be empty; in that case
// Search returns ErrProviderNotConfigured. doer must be non-nil (inject an
// *http.Client in production, a fake in tests).
func NewClient(apiKey string, doer Doer, opts ...Option) *Client {
	c := &Client{
		doer:      doer,
		apiKey:    strings.TrimSpace(apiKey),
		baseURL:   defaultBaseURL,
		userAgent: fmt.Sprintf("BirdNET-Go (%s)", branding.RepoURL()),
		limiter:   rate.NewLimiter(rate.Every(defaultRateInterval), defaultRateBurst),
		cacheTTL:  defaultCacheTTL,
		clock:     time.Now,
		cache:     make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Configured reports whether the client has an API key and can query the
// provider.
func (c *Client) Configured() bool { return c.apiKey != "" }

// Search returns the reference recordings Xeno-canto holds for a scientific
// name, unranked (the caller ranks with detection-specific Preferences).
// Results are cached per species for the configured TTL. It returns
// ErrProviderNotConfigured when no API key is set.
func (c *Client) Search(ctx context.Context, scientificName string) ([]Recording, error) {
	name := strings.TrimSpace(scientificName)
	if name == "" {
		return nil, errors.Newf("scientific name cannot be empty").
			Component("reference").
			Category(errors.CategoryValidation).
			Build()
	}
	if !c.Configured() {
		return nil, ErrProviderNotConfigured
	}

	key := strings.ToLower(name)
	if cached, ok := c.cached(key); ok {
		return cached, nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, errors.New(err).
			Component("reference").
			Category(errors.CategoryLimit).
			Context("operation", "rate_limit_wait").
			Build()
	}

	recordings, err := c.fetch(ctx, name)
	if err != nil {
		return nil, err
	}
	c.store(key, recordings)
	return recordings, nil
}

// fetch performs the HTTP request and maps the response to []Recording.
func (c *Client) fetch(ctx context.Context, scientificName string) ([]Recording, error) {
	endpoint, err := c.buildURL(scientificName)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, errors.New(err).
			Component("reference").
			Category(errors.CategoryNetwork).
			Context("operation", "build_request").
			Build()
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, errors.New(err).
			Component("reference").
			Category(errors.CategoryNetwork).
			Context("operation", "xeno_canto_request").
			Build()
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("xeno-canto returned status %d", resp.StatusCode).
			Component("reference").
			Category(errors.CategoryHTTP).
			Context("status_code", resp.StatusCode).
			Build()
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, errors.New(err).
			Component("reference").
			Category(errors.CategoryNetwork).
			Context("operation", "read_response").
			Build()
	}

	var parsed xcResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New(err).
			Component("reference").
			Category(errors.CategoryFileParsing).
			Context("operation", "parse_response").
			Build()
	}

	recordings := make([]Recording, 0, len(parsed.Recordings))
	for i := range parsed.Recordings {
		recordings = append(recordings, toRecording(&parsed.Recordings[i]))
	}
	return recordings, nil
}

// buildURL assembles the request URL with the species query and API key.
func (c *Client) buildURL(scientificName string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", errors.New(err).
			Component("reference").
			Category(errors.CategoryConfiguration).
			Context("operation", "parse_base_url").
			Build()
	}
	q := u.Query()
	q.Set("query", buildSpeciesQuery(scientificName))
	q.Set("key", c.apiKey)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// buildSpeciesQuery builds a Xeno-canto tag query for a scientific name:
// `gen:"Genus" sp:"species"` when both parts are present, otherwise a plain
// free-text query.
func buildSpeciesQuery(scientificName string) string {
	fields := strings.Fields(scientificName)
	switch len(fields) {
	case 0:
		return ""
	case 1:
		return fields[0]
	default:
		return fmt.Sprintf("gen:%q sp:%q", fields[0], fields[1])
	}
}

func (c *Client) cached(key string) ([]Recording, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || !c.clock().Before(entry.expiresAt) {
		return nil, false
	}
	// Return a copy so callers cannot mutate the cached slice.
	out := make([]Recording, len(entry.recordings))
	copy(out, entry.recordings)
	return out, true
}

func (c *Client) store(key string, recordings []Recording) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{
		recordings: recordings,
		expiresAt:  c.clock().Add(c.cacheTTL),
	}
}

// xcResponse and xcRecording model the subset of the Xeno-canto API v3 response
// this package uses. Unused fields are ignored by the JSON decoder.
type xcResponse struct {
	Recordings []xcRecording `json:"recordings"`
}

type xcRecording struct {
	ID       string   `json:"id"`
	Gen      string   `json:"gen"`
	Sp       string   `json:"sp"`
	Ssp      string   `json:"ssp"`
	En       string   `json:"en"`
	Rec      string   `json:"rec"`
	Cnt      string   `json:"cnt"`
	Loc      string   `json:"loc"`
	Type     string   `json:"type"`
	URL      string   `json:"url"`
	File     string   `json:"file"`
	FileName string   `json:"file-name"`
	Lic      string   `json:"lic"`
	Q        string   `json:"q"`
	Length   string   `json:"length"`
	Also     []string `json:"also"`
	Date     string   `json:"date"`
	Sono     xcSono   `json:"sono"`
}

// xcSono holds Xeno-canto's pre-rendered sonogram ("sound picture") images at
// several sizes.
type xcSono struct {
	Small string `json:"small"`
	Med   string `json:"med"`
	Large string `json:"large"`
	Full  string `json:"full"`
}

// bestSonogram picks the most detailed available sonogram image.
func bestSonogram(s xcSono) string {
	for _, u := range []string{s.Large, s.Med, s.Full, s.Small} {
		if u != "" {
			return normalizeProviderURL(u)
		}
	}
	return ""
}

// toRecording maps a raw Xeno-canto recording to the provider-neutral Recording.
func toRecording(x *xcRecording) Recording {
	licenseName, licenseURL := ParseLicense(x.Lic)
	return Recording{
		ID:             x.ID,
		ScientificName: strings.TrimSpace(x.Gen + " " + x.Sp),
		Subspecies:     x.Ssp,
		CommonName:     x.En,
		Recordist:      x.Rec,
		Country:        x.Cnt,
		Location:       x.Loc,
		CallType:       x.Type,
		PageURL:        normalizeProviderURL(x.URL),
		AudioURL:       normalizeProviderURL(x.File),
		SonogramURL:    bestSonogram(x.Sono),
		FileName:       x.FileName,
		LicenseName:    licenseName,
		LicenseURL:     licenseURL,
		Quality:        x.Q,
		Length:         x.Length,
		Background:     x.Also,
		Date:           x.Date,
		SourceProvider: ProviderXenoCanto,
	}
}

// normalizeProviderURL upgrades Xeno-canto's protocol-relative URLs to https.
func normalizeProviderURL(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
