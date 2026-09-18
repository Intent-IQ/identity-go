// Command benchmark replays OpenRTB bid requests from a JSONL fixture through
// the enrichment flow and writes one JSON file per request with the S2S request
// URL, the raw S2S response, and the resulting enrichment outcome.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/prebid/openrtb/v20/openrtb2"
)

// Fixture lines are single-line bid requests; the cap only guards against a corrupt file.
const maxLineSize = 16 << 20

var errLineTooLong = fmt.Errorf("fixture line exceeds %d bytes", maxLineSize)

type config struct {
	fixture      string
	out          string
	endpoint     string
	partnerID    string
	timeout      time.Duration
	concurrency  int
	limit        int
	skip         int
	shardSize    int
	verbose      bool
	overrides    stringList
	drops        stringList
	requires     stringList
	cacheDir     string
	cacheTTL     time.Duration
	cacheMaxKeys int
}

// stringList collects a repeatable flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			*l = append(*l, trimmed)
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("fixture replay failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config{}
	flag.StringVar(&cfg.fixture, "fixture", "", "path to the JSONL bid request fixture (required)")
	flag.StringVar(&cfg.out, "out", "", "directory to write response JSON files into (required)")
	flag.StringVar(&cfg.endpoint, "endpoint", "", "IIQ S2S endpoint (required)")
	flag.StringVar(&cfg.partnerID, "partner", "", "IIQ partner ID (required)")
	flag.DurationVar(&cfg.timeout, "timeout", 1500*time.Millisecond, "per-request S2S timeout")
	flag.IntVar(&cfg.concurrency, "concurrency", 8, "number of concurrent workers")
	flag.IntVar(&cfg.limit, "limit", 1000, "number of requests to replay; 0 replays the whole fixture")
	flag.IntVar(&cfg.skip, "skip", 0, "number of leading fixture lines to skip")
	flag.IntVar(&cfg.shardSize, "shard-size", 1000, "files per output subdirectory")
	flag.BoolVar(&cfg.verbose, "verbose", false, "log enrichment warnings")
	flag.Var(&cfg.overrides, "param", "add or replace a query parameter as key=value; repeatable")
	flag.Var(&cfg.drops, "drop", "remove a query parameter before sending; repeatable, comma-separated")
	flag.StringVar(&cfg.cacheDir, "cache", "", "enable the identity cache, persisting entries in this directory so a re-run can hit them")
	flag.DurationVar(&cfg.cacheTTL, "cache-ttl", time.Hour, "cache TTL used when the API returns none")
	flag.IntVar(&cfg.cacheMaxKeys, "cache-max-keys", 10, "maximum cache aliases per auction")
	flag.Var(&cfg.requires, "require", "only replay requests whose built URL carries all of these parameters; repeatable, comma-separated")
	flag.Parse()

	if cfg.fixture == "" || cfg.out == "" || cfg.endpoint == "" || cfg.partnerID == "" {
		return errors.New("-fixture, -out, -endpoint and -partner are required")
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = 1
	}
	if cfg.shardSize < 1 {
		cfg.shardSize = 1000
	}

	enricher, err := newEnricher(cfg)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(cfg.out); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	fixture, err := os.Open(cfg.fixture)
	if err != nil {
		return fmt.Errorf("open fixture: %w", err)
	}
	defer fixture.Close()

	stats, err := replay(cfg, enricher, fixture)
	if err != nil {
		return err
	}
	if err := writeSummary(cfg, stats); err != nil {
		return err
	}
	slog.Info("replay finished",
		"replayed", stats.total,
		"enriched", stats.outcomes[string(enrichment.OutcomeEnriched)],
		"failed", stats.failed,
		"output", cfg.out,
	)
	return nil
}

func newEnricher(cfg config) (enrichment.Enricher, error) {
	// The default transport keeps only 2 idle connections per host, which stalls higher concurrency.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = cfg.concurrency * 2
	transport.MaxIdleConnsPerHost = cfg.concurrency * 2

	overrides, err := parseOverrides(cfg.overrides)
	if err != nil {
		return nil, err
	}
	dependencies := enrichment.Dependencies{
		S2S: recordingS2S{
			inner:     s2s.NewClient(&http.Client{Transport: transport}),
			overrides: overrides,
			drops:     cfg.drops,
			requires:  cfg.requires,
		},
		Metrics: cacheMetrics,
	}
	if cfg.verbose {
		dependencies.Logger = slogLogger{}
	}

	// Without -cache every fixture line reaches the S2S API.
	if cfg.cacheDir == "" {
		return enrichment.New(dependencies, 0)
	}

	store, err := newFileStore(cfg.cacheDir)
	if err != nil {
		return nil, fmt.Errorf("create cache store: %w", err)
	}
	identityCache, err := identitycache.New(identitycache.Dependencies{
		Store: store,
		Clock: clock.RealClock{},
	}, cacheConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("create identity cache: %w", err)
	}
	dependencies.Cache = identityCache
	return enrichment.New(dependencies, cfg.cacheMaxKeys)
}

// cacheConfig mirrors the ceilings the example host config uses.
func cacheConfig(cfg config) identitycache.Config {
	return identitycache.Config{
		Enabled:                     true,
		Provider:                    "file",
		TTLSeconds:                  int(cfg.cacheTTL.Seconds()),
		MaxKeys:                     cfg.cacheMaxKeys,
		MaxSize:                     1 << 20,
		TTLCeilingFirstPartySeconds: 86400,
		TTLCeilingThirdPartySeconds: 43200,
		TTLCeilingDeviceSeconds:     900,
		NegativeTTLSeconds:          300,
	}
}

type job struct {
	index int
	line  []byte
}

func replay(cfg config, enricher enrichment.Enricher, fixture io.Reader) (*statistics, error) {
	jobs := make(chan job, cfg.concurrency*2)
	stats := &statistics{outcomes: map[string]int{}, errorKinds: map[string]int{}}
	var firstWriteError error
	var writeErrorMutex sync.Mutex

	var workers sync.WaitGroup
	for range cfg.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for current := range jobs {
				result := replayOne(cfg, enricher, current)
				stats.observe(result)
				if result.Skipped != "" {
					continue
				}
				if err := writeRecord(cfg, result); err != nil {
					slog.Error("write response failed", "index", current.index, "error", err)
					writeErrorMutex.Lock()
					if firstWriteError == nil {
						firstWriteError = fmt.Errorf("write response %d: %w", current.index, err)
					}
					writeErrorMutex.Unlock()
				}
			}
		}()
	}

	reader := bufio.NewReaderSize(fixture, 1<<20)
	var readError error
	for index := 0; ; index++ {
		line, err := readLine(reader)
		if len(line) == 0 && err != nil {
			if !errors.Is(err, io.EOF) {
				readError = fmt.Errorf("read fixture line %d: %w", index, err)
			}
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			readError = fmt.Errorf("read fixture line %d: %w", index, err)
			break
		}
		if index < cfg.skip {
			continue
		}
		if cfg.limit > 0 && index-cfg.skip >= cfg.limit {
			break
		}
		jobs <- job{index: index, line: line}
		if progress := index - cfg.skip + 1; progress%1000 == 0 {
			slog.Info("replay progress", "dispatched", progress)
		}
	}
	close(jobs)
	workers.Wait()
	if readError != nil {
		return stats, readError
	}
	if firstWriteError != nil {
		return stats, firstWriteError
	}
	return stats, nil
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		line = append(line, chunk...)
		if err != nil {
			return line, err
		}
		if len(line) > maxLineSize {
			return nil, errLineTooLong
		}
		if !isPrefix {
			return line, nil
		}
	}
}

func replayOne(cfg config, enricher enrichment.Enricher, current job) record {
	result := record{Index: current.index}

	var auction openrtb2.BidRequest
	if err := json.Unmarshal(current.line, &auction); err != nil {
		result.Error = &errorRecord{Kind: "fixture_parse", Message: err.Error()}
		return result
	}
	liftExtEIDs(&auction)
	result.AuctionID = auction.ID
	result.Reference = reference(&auction)

	captured := &capture{}
	ctx := context.WithValue(context.Background(), captureKey{}, captured)
	started := time.Now()
	enriched, err := enricher.Enrich(ctx, enrichment.Request{
		PartnerID:    cfg.partnerID,
		Endpoint:     cfg.endpoint,
		Auction:      &auction,
		Timeout:      cfg.timeout,
		CacheEnabled: cfg.cacheDir != "",
	})
	result.DurationMS = float64(time.Since(started).Microseconds()) / 1000
	result.RequestURL = captured.requestURL
	result.GDPRConsent = captured.consent
	// A cache-served auction never reaches the recording S2S client.
	result.FromCache = cfg.cacheDir != "" && captured.requestURL == ""

	if captured.skipped != "" {
		result.Skipped = captured.skipped
		return result
	}
	if err != nil {
		kind, status := classify(err)
		result.Error = &errorRecord{Kind: kind, Status: status, Message: err.Error()}
		return result
	}

	result.Outcome = string(enriched.Outcome)
	if !result.FromCache {
		result.Response = &responseRecord{
			Status:     captured.response.Status,
			Data:       captured.response.Data,
			CacheTTLMS: captured.response.CacheTTL,
			ABTestUUID: captured.response.ABTestUUID,
			TC:         captured.response.TC,
		}
	}
	result.Result = &resultRecord{
		EIDs:             enriched.EIDs,
		CacheTTLMS:       enriched.CacheTTL.Milliseconds(),
		ABTestUUID:       enriched.ABTestUUID,
		TerminationCause: enriched.TerminationCause,
	}
	return result
}

// liftExtEIDs moves user.ext.eids to user.eids. Prebid Server performs the same
// migration before modules run; the fixture holds the pre-migration shape, and
// without it the iiquid parameter is never sent.
func liftExtEIDs(auction *openrtb2.BidRequest) {
	user := auction.User
	if user == nil || len(user.EIDs) > 0 || len(user.Ext) == 0 {
		return
	}
	var extension struct {
		EIDs []openrtb2.EID `json:"eids"`
	}
	if json.Unmarshal(user.Ext, &extension) != nil || len(extension.EIDs) == 0 {
		return
	}
	user.EIDs = extension.EIDs
}

func reference(auction *openrtb2.BidRequest) string {
	if site := auction.Site; site != nil {
		if site.Domain != "" {
			return site.Domain
		}
		return site.Page
	}
	if app := auction.App; app != nil {
		if app.Bundle != "" {
			return app.Bundle
		}
		return app.Name
	}
	return ""
}

func classify(err error) (kind string, status int) {
	kind, _ = iiqapi.ErrorLabels(err)
	var apiError *iiqapi.Error
	if errors.As(err, &apiError) {
		status = apiError.Status
	}
	return kind, status
}

type record struct {
	Index       int             `json:"index"`
	AuctionID   string          `json:"auction_id"`
	Reference   string          `json:"reference,omitempty"`
	RequestURL  string          `json:"s2s_request_url,omitempty"`
	GDPRConsent string          `json:"gdpr_consent_header,omitempty"`
	DurationMS  float64         `json:"duration_ms"`
	Outcome     string          `json:"outcome,omitempty"`
	Skipped     string          `json:"skipped_missing_param,omitempty"`
	FromCache   bool            `json:"served_from_cache,omitempty"`
	Response    *responseRecord `json:"s2s_response,omitempty"`
	Result      *resultRecord   `json:"enrichment_result,omitempty"`
	Error       *errorRecord    `json:"error,omitempty"`
}

type responseRecord struct {
	Status     int             `json:"status"`
	Data       json.RawMessage `json:"data,omitempty"`
	CacheTTLMS *int64          `json:"cttl,omitempty"`
	ABTestUUID string          `json:"ab_test_uuid,omitempty"`
	TC         *int64          `json:"tc,omitempty"`
}

type resultRecord struct {
	EIDs             []openrtb2.EID `json:"eids"`
	CacheTTLMS       int64          `json:"cache_ttl_ms"`
	ABTestUUID       string         `json:"ab_test_uuid,omitempty"`
	TerminationCause *int64         `json:"termination_cause,omitempty"`
}

type errorRecord struct {
	Kind    string `json:"kind"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
}

func writeRecord(cfg config, result record) error {
	shard := filepath.Join(cfg.out, fmt.Sprintf("part-%05d", result.Index/cfg.shardSize))
	if err := ensurePrivateDir(shard); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(shard, fmt.Sprintf("%09d.json", result.Index)), append(encoded, '\n'))
}

type statistics struct {
	mutex      sync.Mutex
	total      int
	failed     int
	filtered   int
	fromCache  int
	eids       int
	durationMS float64
	outcomes   map[string]int
	errorKinds map[string]int
}

func (s *statistics) observe(result record) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if result.Skipped != "" {
		s.filtered++
		return
	}
	s.total++
	s.durationMS += result.DurationMS
	if result.FromCache {
		s.fromCache++
	}
	if result.Error != nil {
		s.failed++
		s.errorKinds[result.Error.Kind]++
		return
	}
	s.outcomes[result.Outcome]++
	if result.Result != nil {
		s.eids += len(result.Result.EIDs)
	}
}

func writeSummary(cfg config, stats *statistics) error {
	summary := struct {
		Fixture         string         `json:"fixture"`
		Endpoint        string         `json:"endpoint"`
		PartnerID       string         `json:"partner_id"`
		Skipped         int            `json:"skipped"`
		Replayed        int            `json:"replayed"`
		Filtered        int            `json:"filtered_out"`
		Failed          int            `json:"failed"`
		ResolvedEIDs    int            `json:"resolved_eids"`
		AvgDurationMS   float64        `json:"avg_duration_ms"`
		ServedFromCache int            `json:"served_from_cache"`
		CacheLookups    map[string]int `json:"cache_lookups,omitempty"`
		Outcomes        map[string]int `json:"outcomes"`
		ErrorKinds      map[string]int `json:"error_kinds"`
	}{
		Fixture:         cfg.fixture,
		Endpoint:        cfg.endpoint,
		PartnerID:       cfg.partnerID,
		Skipped:         cfg.skip,
		Replayed:        stats.total,
		Filtered:        stats.filtered,
		Failed:          stats.failed,
		ResolvedEIDs:    stats.eids,
		ServedFromCache: stats.fromCache,
		CacheLookups:    cacheMetrics.snapshot(),
		Outcomes:        stats.outcomes,
		ErrorKinds:      stats.errorKinds,
	}
	if stats.total > 0 {
		summary.AvgDurationMS = stats.durationMS / float64(stats.total)
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(cfg.out, "summary.json"), append(encoded, '\n'))
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func writePrivateFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// cacheRecorder counts cache lookups for the run summary. The Metrics interface
// has no per-request handle, so attribution per record comes from whether the
// S2S call happened at all.
type cacheRecorder struct {
	mutex   sync.Mutex
	lookups map[string]int
}

func newCacheRecorder() *cacheRecorder {
	return &cacheRecorder{lookups: map[string]int{}}
}

func (r *cacheRecorder) CacheLookup(_ string, result enrichment.CacheLookupResult, layer enrichment.CacheLayer) {
	label := string(result) + "_layer:" + layer.Token()
	r.mutex.Lock()
	r.lookups[label]++
	r.mutex.Unlock()
}

func (r *cacheRecorder) snapshot() map[string]int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	copied := make(map[string]int, len(r.lookups))
	for label, count := range r.lookups {
		copied[label] = count
	}
	return copied
}

func (*cacheRecorder) Request(string)                                   {}
func (*cacheRecorder) Enriched(string)                                  {}
func (*cacheRecorder) NotEnriched(string, enrichment.NotEnrichedReason) {}
func (*cacheRecorder) APIRequestDuration(string, time.Duration)         {}
func (*cacheRecorder) APISuccess(string)                                {}
func (*cacheRecorder) APIError(string, string, int)                     {}

var cacheMetrics = newCacheRecorder()

type captureKey struct{}

type capture struct {
	requestURL string
	consent    string
	response   s2s.Response
	skipped    string
}

// recordingS2S keeps the URL built by the enrichment flow and the raw response so
// the replay can persist both without changing the library contract. It also
// applies -param/-drop/-require, which need the built URL and so cannot act earlier.
type recordingS2S struct {
	inner     s2s.API
	overrides map[string]string
	drops     []string
	requires  []string
}

// errSkipped reports a request the -require filter excluded before any HTTP call.
var errSkipped = errors.New("skipped by -require filter")

func (r recordingS2S) Resolve(ctx context.Context, requestURL, consent string) (s2s.Response, error) {
	requestURL = rewriteQuery(requestURL, r.overrides, r.drops)
	recorded, _ := ctx.Value(captureKey{}).(*capture)
	if recorded != nil {
		recorded.requestURL = requestURL
		recorded.consent = consent
	}
	if missing := missingParameters(requestURL, r.requires); missing != "" {
		if recorded != nil {
			recorded.skipped = missing
		}
		return s2s.Response{}, errSkipped
	}

	response, err := r.inner.Resolve(ctx, requestURL, consent)
	if recorded != nil {
		recorded.response = response
	}
	return response, err
}

func parseOverrides(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	overrides := make(map[string]string, len(values))
	for _, value := range values {
		name, parameter, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("-param %q must be key=value", value)
		}
		overrides[strings.TrimSpace(name)] = parameter
	}
	return overrides, nil
}

// rewriteQuery applies drops and overrides while preserving the original parameter
// order and the %20 space encoding the S2S API is given by the library.
func rewriteQuery(requestURL string, overrides map[string]string, drops []string) string {
	if len(overrides) == 0 && len(drops) == 0 {
		return requestURL
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return requestURL
	}

	dropped := make(map[string]bool, len(drops))
	for _, name := range drops {
		dropped[name] = true
	}
	applied := make(map[string]bool, len(overrides))

	var builder strings.Builder
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		name, _, _ := strings.Cut(pair, "=")
		decoded, err := url.QueryUnescape(name)
		if err != nil {
			decoded = name
		}
		if dropped[decoded] {
			continue
		}
		value := pair
		if replacement, ok := overrides[decoded]; ok {
			value = decoded + "=" + encodeParameter(replacement)
			applied[decoded] = true
		}
		appendPair(&builder, value)
	}
	remaining := make([]string, 0, len(overrides))
	for name := range overrides {
		if applied[name] || dropped[name] {
			continue
		}
		remaining = append(remaining, name)
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		value := overrides[name]
		appendPair(&builder, name+"="+encodeParameter(value))
	}

	parsed.RawQuery = builder.String()
	return parsed.String()
}

func appendPair(builder *strings.Builder, pair string) {
	if builder.Len() > 0 {
		builder.WriteByte('&')
	}
	builder.WriteString(pair)
}

func encodeParameter(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// missingParameters returns the first required parameter absent from the URL.
func missingParameters(requestURL string, requires []string) string {
	if len(requires) == 0 {
		return ""
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	for _, name := range requires {
		if query.Get(name) == "" {
			return name
		}
	}
	return ""
}

type slogLogger struct{}

func (slogLogger) Debug(message string) { slog.Debug(message) }
func (slogLogger) Warn(message string)  { slog.Warn(message) }
func (slogLogger) Error(message string) { slog.Error(message) }
