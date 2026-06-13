package dbengines_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/dbengines-cli/dbengines"
)

// Re-export for test convenience.
var (
	DefaultConfig = dbengines.DefaultConfig
	NewClient     = dbengines.NewClient
)

// minimalRankingHTML is a trimmed version of the DB-Engines ranking page
// with three data rows for testing.
const minimalRankingHTML = `<!DOCTYPE HTML>
<html><head><title>DB-Engines Ranking</title></head><body>
<table class=dbi>
<tr><td colspan=99>434 systems in ranking, June 2026
<tr><td class=dbi_header colspan=3>Rank<th class="dbi_header pad-l">DBMS<th class="dbi_header pad-r">Database Model
<tr><td>1.<td class=small>1.<td class="small pad-r">1.<th class="pad-l"><a href="/en/system/Oracle">Oracle</a></th><th class="small pad-r"><a href="/en/article/RDBMS">Relational</a>, Multi-model <span class=info><span class="infobox">Relational DBMS</span></span></th><td class="pad-l">1140.04<td class="small minus">-3.24<td class="small minus">-90.35
<tr><td>2.<td class=small>2.<td class="small pad-r">2.<th class="pad-l"><a href="/en/system/MySQL">MySQL</a></th><th class="small pad-r"><a href="/en/article/RDBMS">Relational</a>, Multi-model <span class=info></span></th><td class="pad-l">856.29<td class="small minus">-0.21<td class="small minus">-97.29
<tr><td>3.<td class=small>3.<td class="small pad-r">3.<th class="pad-l"><a href="/en/system/PostgreSQL">PostgreSQL</a></th><th class="small pad-r"><a href="/en/article/RDBMS">Relational</a>, Multi-model <span class=info></span></th><td class="pad-l">723.11<td class="small plus">+2.10<td class="small minus">-5.20
</table>
</body></html>`

func newTestServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestRanking(t *testing.T) {
	ts := newTestServer(minimalRankingHTML)
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	c := NewClient(cfg)

	systems, err := c.Ranking(context.Background())
	if err != nil {
		t.Fatalf("Ranking() error: %v", err)
	}
	if len(systems) != 3 {
		t.Fatalf("got %d systems, want 3", len(systems))
	}

	// First entry.
	s := systems[0]
	if s.Rank != 1 {
		t.Errorf("rank = %d, want 1", s.Rank)
	}
	if s.Name != "Oracle" {
		t.Errorf("name = %q, want Oracle", s.Name)
	}
	if !strings.Contains(s.Type, "Relational") {
		t.Errorf("type = %q, want Relational in type", s.Type)
	}
	if s.Score != "1140.04" {
		t.Errorf("score = %q, want 1140.04", s.Score)
	}
	if s.Change != "-3.24" {
		t.Errorf("change = %q, want -3.24", s.Change)
	}
	if !strings.Contains(s.URL, "/en/system/Oracle") {
		t.Errorf("URL = %q, want to contain /en/system/Oracle", s.URL)
	}

	// Third entry.
	s3 := systems[2]
	if s3.Rank != 3 {
		t.Errorf("rank = %d, want 3", s3.Rank)
	}
	if s3.Name != "PostgreSQL" {
		t.Errorf("name = %q, want PostgreSQL", s3.Name)
	}
	if s3.Score != "723.11" {
		t.Errorf("score = %q, want 723.11", s3.Score)
	}
	if s3.Change != "+2.10" {
		t.Errorf("change = %q, want +2.10", s3.Change)
	}
}

func TestRankingEmpty(t *testing.T) {
	ts := newTestServer(`<html><body><p>no table</p></body></html>`)
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	c := NewClient(cfg)

	systems, err := c.Ranking(context.Background())
	if err != nil {
		t.Fatalf("Ranking() on empty page error: %v", err)
	}
	if len(systems) != 0 {
		t.Errorf("got %d systems on empty page, want 0", len(systems))
	}
}

func TestRankingRetryOn503(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(minimalRankingHTML))
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := NewClient(cfg)

	systems, err := c.Ranking(context.Background())
	if err != nil {
		t.Fatalf("Ranking() with retries error: %v", err)
	}
	if len(systems) != 3 {
		t.Errorf("got %d systems after retries, want 3", len(systems))
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}
