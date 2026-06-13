// Package dbengines is the library behind the dbe command line:
// the HTTP client, request shaping, and the typed data models for the
// DB-Engines ranking at db-engines.com.
//
// The site returns HTML pages; this package parses them with stdlib strings
// only — no external HTML parser.
package dbengines

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBase      = "https://db-engines.com"
	defaultUserAgent = "dbe/dev (+https://github.com/tamnd/dbengines-cli)"
)

// Config holds constructor parameters for Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   defaultBase,
		UserAgent: defaultUserAgent,
		Rate:      500 * time.Millisecond,
		Retries:   3,
		Timeout:   30 * time.Second,
	}
}

// Client talks to db-engines.com over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	last time.Time
}

// NewClient returns a Client configured from cfg.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBase
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// get fetches a URL with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// Ranking fetches the full DB-Engines ranking page and returns all parsed entries.
func (c *Client) Ranking(ctx context.Context) ([]System, error) {
	rawURL := c.cfg.BaseURL + "/en/ranking"
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return parseRanking(string(body), c.cfg.BaseURL), nil
}

// parseRanking parses the ranking HTML and returns System records.
//
// Real-page row structure (no closing </th> tags, href may be absolute or relative):
//
//	<tr><td>1.<td class=small>1.<td class="small pad-r">1.
//	<th class="pad-l"><a href="https://db-engines.com/en/system/Oracle">Oracle</a>
//	<th class="small pad-r"><a href="...">Relational</a>, Multi-model <span...></span>
//	<td class="pad-l">1140.04<td class="small minus">-3.24<td class="small minus">-90.35
func parseRanking(html, baseURL string) []System {
	// Find the dbi table.
	tableStart := strings.Index(html, "class=dbi")
	if tableStart == -1 {
		tableStart = strings.Index(html, `class="dbi"`)
	}
	if tableStart == -1 {
		return nil
	}
	rest := html[tableStart:]

	var systems []System
	pos := 0
	for {
		// Find next row start — handles both <tr><td> and <tr class=...><td>.
		trIdx := strings.Index(rest[pos:], "<tr")
		if trIdx == -1 {
			break
		}
		trIdx += pos

		// Advance pos past this <tr so next iteration moves forward.
		pos = trIdx + 3

		// Find end of this row: start of next <tr.
		nextTr := strings.Index(rest[pos:], "<tr")
		rowEnd := len(rest)
		if nextTr != -1 {
			rowEnd = pos + nextTr
		}
		row := rest[trIdx:rowEnd]

		// Only data rows have an unattributed <td> as their first cell.
		tdIdx := strings.Index(row, "<td>")
		if tdIdx == -1 {
			continue
		}
		// rowStart is where the content of the first <td> begins.
		_ = tdIdx

		// Only data rows: <td>N. where N is decimal digits.
		// The first <td> content is the rank number.
		rankContent := row[tdIdx+len("<td>"):]
		dotIdx := strings.Index(rankContent, ".")
		if dotIdx <= 0 {
			continue
		}
		rankStr := rankContent[:dotIdx]
		rank := 0
		for _, ch := range rankStr {
			if ch < '0' || ch > '9' {
				rank = -1
				break
			}
			rank = rank*10 + int(ch-'0')
		}
		if rank <= 0 {
			continue
		}

		// Name cell: <th class="pad-l"><a href="...SYSTEM_URL...">NAME</a>
		padLStart := strings.Index(row, `class="pad-l"`)
		if padLStart == -1 {
			continue
		}
		padLRow := row[padLStart:]

		// href may be absolute (real page) or relative (tests).
		sysHref := extractBetween(padLRow, `href="`, `"`)
		if !strings.Contains(sysHref, "/en/system/") {
			continue
		}

		// Name text between "> and </a>.
		anchorMarker := `href="` + sysHref + `">`
		anchorIdx := strings.Index(padLRow, anchorMarker)
		if anchorIdx == -1 {
			continue
		}
		afterAnchor := padLRow[anchorIdx+len(anchorMarker):]
		closeIdx := strings.Index(afterAnchor, "</a>")
		if closeIdx == -1 {
			continue
		}
		name := afterAnchor[:closeIdx]
		if name == "" {
			continue
		}

		// Model type cell: the <th> AFTER the name cell.
		// padLRow starts from inside the name <th>, so the first <th found
		// is the model type cell.
		modelThIdx := strings.Index(padLRow, "<th")
		modelCell := ""
		if modelThIdx != -1 {
			// Skip past the opening <th...> tag to get cell content.
			modelThContent := padLRow[modelThIdx:]
			gtIdx := strings.Index(modelThContent, ">")
			if gtIdx != -1 {
				modelStart := modelThContent[gtIdx+1:]
				// Model cell ends at <td class="pad-l"> (the score cell).
				modelEnd := strings.Index(modelStart, `<td class="pad-l">`)
				if modelEnd == -1 {
					modelEnd = strings.Index(modelStart, "<td")
				}
				if modelEnd != -1 {
					modelCell = modelStart[:modelEnd]
				} else {
					modelCell = modelStart
				}
			}
		}
		modelType := extractModelType(modelCell)

		// Score: value inside <td class="pad-l">...</td> or until next <td.
		score := ""
		scoreMarker := `<td class="pad-l">`
		scoreStart := strings.LastIndex(row, scoreMarker)
		if scoreStart != -1 {
			afterScore := row[scoreStart+len(scoreMarker):]
			scoreEnd := strings.Index(afterScore, "<")
			if scoreEnd != -1 {
				score = strings.TrimSpace(afterScore[:scoreEnd])
			}
		}

		// Change: first <td class="small minus"> or <td class="small plus"> after score cell.
		change := extractChange(row)

		// Build URL: if href is already absolute, use it directly.
		url := sysHref
		if !strings.HasPrefix(sysHref, "http") {
			url = baseURL + sysHref
		}

		systems = append(systems, System{
			Rank:   rank,
			Name:   name,
			Type:   modelType,
			Score:  score,
			Change: change,
			URL:    url,
		})
	}
	return systems
}

// extractModelType parses the model type cell, stripping HTML tags and
// collapsing whitespace. The cell contains links and span elements.
func extractModelType(cell string) string {
	// Remove span elements entirely (they contain tooltip info).
	for {
		spanStart := strings.Index(cell, "<span")
		if spanStart == -1 {
			break
		}
		spanEnd := strings.Index(cell[spanStart:], "</span>")
		if spanEnd == -1 {
			cell = cell[:spanStart]
			break
		}
		// Remove nested spans too — keep advancing spanEnd until the outermost </span>.
		for {
			outerEnd := strings.LastIndex(cell[spanStart:], "</span>")
			if outerEnd <= spanEnd {
				break
			}
			spanEnd = outerEnd
		}
		cell = cell[:spanStart] + cell[spanStart+spanEnd+len("</span>"):]
	}

	// Strip all remaining HTML tags.
	var b strings.Builder
	inTag := false
	for _, ch := range cell {
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			b.WriteRune(ch)
		}
	}
	result := strings.TrimSpace(b.String())
	// Collapse whitespace.
	parts := strings.Fields(result)
	return strings.Join(parts, " ")
}

// extractChange finds the score change value from the row HTML.
func extractChange(row string) string {
	// Look for <td class="small minus"> or <td class="small plus"> or <td class="small">
	// These appear after the main score columns.
	// Find the first occurrence after class="pad-l" for score.
	scoreStart := strings.LastIndex(row, `class="pad-l"`)
	if scoreStart == -1 {
		return ""
	}
	after := row[scoreStart:]

	// First change: first <td class="small..."> after the score.
	tdIdx := strings.Index(after, `<td class="small`)
	if tdIdx == -1 {
		return ""
	}
	changeCell := after[tdIdx:]
	// Extract value between > and <.
	gtIdx := strings.Index(changeCell, ">")
	if gtIdx == -1 {
		return ""
	}
	ltIdx := strings.Index(changeCell[gtIdx:], "<")
	if ltIdx == -1 {
		return ""
	}
	return strings.TrimSpace(changeCell[gtIdx+1 : gtIdx+ltIdx])
}

// extractBetween returns the string between open and close in s.
func extractBetween(s, open, close string) string {
	i := strings.Index(s, open)
	if i == -1 {
		return ""
	}
	i += len(open)
	j := strings.Index(s[i:], close)
	if j == -1 {
		return ""
	}
	return s[i : i+j]
}
