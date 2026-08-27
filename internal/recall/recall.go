package recall

import (
	"fmt"
	"strings"
	"time"

	"github.com/flazouh/mneme/internal/memory"
	"github.com/flazouh/mneme/internal/store"
)

type Pack struct {
	Chars           int      `json:"chars"`
	ContextPack     string   `json:"context_pack"`
	Count           int      `json:"count"`
	EstimatedTokens int      `json:"estimated_tokens"`
	IDs             []string `json:"ids,omitempty"`
}

func Compact(hits []store.Hit, maxChars int, now time.Time) Pack {
	if maxChars <= 0 {
		maxChars = 1200
	}
	var lines []string
	var ids []string
	used := 0
	for i, hit := range hits {
		text := oneLine(hit.Record.Text)
		if text == "" {
			continue
		}
		labels := recordLabels(hit.Record, hit.Score, now)
		prefix := fmt.Sprintf("%d. ", i+1)
		if len(labels) > 0 {
			prefix += "[" + strings.Join(labels, ", ") + "] "
		}
		available := maxChars - used - len(prefix)
		if available <= 0 {
			break
		}
		if len(text) > available {
			text = clip(text, available)
		}
		line := prefix + text
		lines = append(lines, line)
		ids = append(ids, hit.Record.ID)
		used += len(line) + 1
		if used >= maxChars {
			break
		}
	}
	body := strings.Join(lines, "\n")
	return Pack{
		Chars:           len(body),
		ContextPack:     body,
		Count:           len(lines),
		EstimatedTokens: (len(body) + 3) / 4,
		IDs:             ids,
	}
}

func recordLabels(rec memory.Record, score float64, now time.Time) []string {
	var out []string
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			out = append(out, k+"="+clip(v, 80))
		}
	}
	add("type", string(rec.Type))
	add("scope", string(rec.Scope))
	add("project", rec.Project)
	add("source", rec.Source)
	add("lifecycle", string(rec.Lifecycle))
	if score > 0 {
		out = append(out, fmt.Sprintf("score=%.3f", score))
	}
	if !rec.CreatedAt.IsZero() {
		days := int(now.Sub(rec.CreatedAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		out = append(out, fmt.Sprintf("age_days=%d", days))
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
