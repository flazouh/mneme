package gate

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/flazouh/mneme/internal/errcat"
)

type Decision string

const (
	Yes    Decision = "yes"
	No     Decision = "no"
	Review Decision = "review"
)

type Result struct {
	Decision Decision `json:"decision"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Text     string   `json:"text"`
}

var (
	secretPatterns = compileAll(
		`(?i)\b[A-Z0-9_]*(api[_-]?key|secret|token|password|private[_-]?key)[A-Z0-9_]*\s*[:=]\s*\S+`,
		`sk-[A-Za-z0-9_-]{20,}`,
		`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
	)
	injectionPatterns = compileAll(
		`(?i)\bignore (all )?(previous|prior|above|system|developer) (instructions|context|messages?)\b`,
		`(?i)\b(disregard|override) (previous|prior|system|developer) (instructions|context|messages?)\b`,
		`(?i)\b(system|developer) prompt\b`,
		`(?i)\bfollow these instructions\b`,
		`(?i)\breveal (the )?(system|developer) (prompt|message|instructions)\b`,
	)
	eventPatterns = compileAll(
		`(?i)\bPR\s*#?\d+\b.*\b(merged|closed|opened|passed|failed)\b`,
		`(?i)\b(branch|tag)\b.*\b(deleted|created|merged)\b`,
		`(?i)\bCI\b.*\b(passed|failed|green|red)\b`,
		`(?i)\btests?\b.*\b(passed|green|failed)\b.*\b(commit|task|completed|successfully)\b`,
		`(?i)\btask completed successfully\b`,
	)
	transcriptPatterns = compileAll(
		`(?im)^\s*user:\s(.|\n)*^\s*assistant:\s`,
		`(?im)^\s*tool output:\s`,
		`(?im)^\s*(system|developer|assistant|user):\s`,
	)
	explicitPhrases = []string{"remember this", "remember that", "always ", "never ", "make it a learning"}
	durableWords    = []string{
		"prefer", "preference", "decision", "decided", "standard", "workflow",
		"lesson", "constraint", "rule", "setup", "project", "shared memory", "store",
	}
	temporaryWords = []string{"today", "right now", "temporary", "draft", "brainstorm", "maybe", "todo"}
)

func compileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

func anyMatch(patterns []*regexp.Regexp, text string) bool {
	for _, p := range patterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// Evaluate scores a candidate. Decision "no" is a hard reject.
func Evaluate(text string) Result {
	compact := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(compact)
	var reasons, warnings []string
	score := 0

	if anyMatch(secretPatterns, compact) {
		warnings = append(warnings, "possible secret or credential")
		score -= 5
	}
	if anyMatch(injectionPatterns, compact) {
		warnings = append(warnings, "possible prompt injection")
		score -= 5
	}
	if anyMatch(eventPatterns, compact) {
		warnings = append(warnings, "rediscoverable event; store the reusable lesson instead")
		score -= 5
	}
	if anyMatch(transcriptPatterns, text) {
		warnings = append(warnings, "raw transcript or role-tagged conversation")
		score -= 5
	}

	for _, phrase := range explicitPhrases {
		if strings.Contains(lower, phrase) {
			reasons = append(reasons, "explicit memory signal")
			score += 3
			break
		}
	}

	var hits []string
	seen := map[string]struct{}{}
	for _, word := range durableWords {
		if strings.Contains(lower, word) {
			if _, ok := seen[word]; ok {
				continue
			}
			seen[word] = struct{}{}
			hits = append(hits, word)
		}
	}
	if len(hits) > 0 {
		shown := hits
		if len(shown) > 5 {
			shown = shown[:5]
		}
		reasons = append(reasons, "contains durable-memory language: "+strings.Join(shown, ", "))
		bonus := len(seen)
		if bonus > 3 {
			bonus = 3
		}
		score += bonus
	}

	n := utf8.RuneCountInString(compact)
	if n < 25 {
		warnings = append(warnings, "too short to be useful")
		score -= 2
	} else if n > 1200 {
		warnings = append(warnings, "long memory candidate; compress before saving")
		score -= 1
	}

	var tempHits []string
	for _, word := range temporaryWords {
		if strings.Contains(lower, word) {
			tempHits = append(tempHits, word)
		}
	}
	if len(tempHits) > 0 {
		warnings = append(warnings, "may be temporary or speculative")
		score -= 1
	}

	hasBlocking := false
	for _, w := range warnings {
		if strings.Contains(w, "secret") || strings.Contains(w, "credential") ||
			strings.Contains(w, "prompt injection") || strings.Contains(w, "rediscoverable event") ||
			strings.Contains(w, "raw transcript") {
			hasBlocking = true
			break
		}
	}
	hasDurable := false
	for _, r := range reasons {
		if strings.HasPrefix(r, "contains durable-memory language") {
			hasDurable = true
			break
		}
	}
	explicitOnly := false
	for _, r := range reasons {
		if r == "explicit memory signal" && !hasDurable {
			explicitOnly = true
			break
		}
	}

	decision := Review
	if score >= 2 {
		decision = Yes
	}
	if len(tempHits) > 0 && explicitOnly {
		decision = Review
	}
	if n < 25 {
		decision = No
	}
	if hasBlocking {
		decision = No
	}

	return Result{
		Decision: decision,
		Score:    score,
		Reasons:  reasons,
		Warnings: warnings,
		Text:     compact,
	}
}

// RejectBlocked returns a write_rejected error when the gate decision is no.
func RejectBlocked(text string) (Result, error) {
	result := Evaluate(text)
	if result.Decision == No {
		msg := "low signal"
		if len(result.Warnings) > 0 {
			msg = strings.Join(result.Warnings, "; ")
		}
		return result, errcat.New(errcat.WriteRejected, "memory rejected by gate: "+msg)
	}
	return result, nil
}
