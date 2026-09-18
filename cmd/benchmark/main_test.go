package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Intent-IQ/identity-go/enrichment"
)

type stubEnricher struct {
	result enrichment.Result
	err    error
}

func (stub stubEnricher) Enrich(context.Context, enrichment.Request) (enrichment.Result, error) {
	return stub.result, stub.err
}

func TestReadLineRejectsOversizedLine(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", maxLineSize+1)+"\n"), 1024)

	line, err := readLine(reader)

	if !errors.Is(err, errLineTooLong) {
		t.Fatalf("readLine() error = %v, want %v", err, errLineTooLong)
	}
	if line != nil {
		t.Fatalf("readLine() returned %d bytes for an oversized line", len(line))
	}
}

func TestReadLineAllowsMaximumSize(t *testing.T) {
	want := bytes.Repeat([]byte("x"), maxLineSize)
	reader := bufio.NewReaderSize(bytes.NewReader(append(want, '\n')), 1024)

	got, err := readLine(reader)

	if err != nil {
		t.Fatalf("readLine() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("readLine() returned %d bytes, want %d", len(got), len(want))
	}
}

func TestSampleFixtureIsValid(t *testing.T) {
	fixture, err := os.Open(filepath.Join("testdata", "sample.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()

	stats, err := replay(
		config{concurrency: 1, out: t.TempDir(), shardSize: 1},
		stubEnricher{result: enrichment.Result{Outcome: enrichment.OutcomeNoIDs}},
		fixture,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.total != 1 || stats.failed != 0 {
		t.Fatalf("sample fixture stats = total %d, failed %d; want total 1, failed 0", stats.total, stats.failed)
	}
}

func TestReplayReturnsFixtureReadError(t *testing.T) {
	want := errors.New("fixture unavailable")

	_, err := replay(config{concurrency: 1}, stubEnricher{}, errorReader{err: want})

	if !errors.Is(err, want) {
		t.Fatalf("replay() error = %v, want wrapped %v", err, want)
	}
}

func TestReplayReturnsRecordWriteError(t *testing.T) {
	temp := t.TempDir()
	out := filepath.Join(temp, "not-a-directory")
	if err := os.WriteFile(out, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := replay(
		config{concurrency: 1, out: out, shardSize: 1},
		stubEnricher{result: enrichment.Result{Outcome: enrichment.OutcomeNoIDs}},
		strings.NewReader("{}\n"),
	)

	if err == nil || !strings.Contains(err.Error(), "write response 0") {
		t.Fatalf("replay() error = %v, want record write error", err)
	}
}

func TestReplayOneOmitsS2SResponseForCacheHit(t *testing.T) {
	result := replayOne(
		config{cacheDir: "enabled"},
		stubEnricher{result: enrichment.Result{Outcome: enrichment.OutcomeEnriched}},
		job{line: []byte(`{}`)},
	)

	if !result.FromCache {
		t.Fatal("replayOne() did not mark a request without an S2S call as cached")
	}
	if result.Response != nil {
		t.Fatalf("replayOne() response = %#v, want nil for cache hit", result.Response)
	}
}

func TestParseOverridesTrimsNames(t *testing.T) {
	got, err := parseOverrides([]string{" key =value"})
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != "value" {
		t.Fatalf("parseOverrides() = %#v, want trimmed key", got)
	}
}

func TestRewriteQueryAppendsOverridesDeterministically(t *testing.T) {
	got := rewriteQuery("https://example.test/path?existing=1", map[string]string{
		"z": "last",
		"a": "first",
	}, nil)
	want := "https://example.test/path?existing=1&a=first&z=last"
	if got != want {
		t.Fatalf("rewriteQuery() = %q, want %q", got, want)
	}
}

func TestWriteRecordUsesPrivatePermissions(t *testing.T) {
	cfg := config{out: t.TempDir(), shardSize: 1}
	path := filepath.Join(cfg.out, "part-00000", "000000000.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeRecord(cfg, record{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("record permissions = %o, want 600", permissions)
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }
