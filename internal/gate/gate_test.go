package gate_test

import (
	"strings"
	"testing"

	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/gate"
)

func TestRejectsRediscoverableEvents(t *testing.T) {
	t.Parallel()
	candidates := []string{
		"PR #1500 was merged after all CI checks passed.",
		"The feature branch codex/foo was deleted after merge.",
		"CI passed on the latest commit.",
		"Task completed successfully and tests are green.",
	}
	for _, text := range candidates {
		result := gate.Evaluate(text)
		if result.Decision != gate.No {
			t.Fatalf("%q: got %s want no", text, result.Decision)
		}
		found := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "rediscoverable event") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q: missing rediscoverable warning: %v", text, result.Warnings)
		}
	}
}

func TestRejectsRawTranscript(t *testing.T) {
	t.Parallel()
	text := "User: please fix this\nAssistant: I ran tests\nTool output: 200 lines of logs"
	result := gate.Evaluate(text)
	if result.Decision != gate.No {
		t.Fatalf("got %s want no", result.Decision)
	}
}

func TestReusableLessonPasses(t *testing.T) {
	t.Parallel()
	text := "Lesson: when Ori import tests fail, check the adapter boundary fixture before changing provider code."
	result := gate.Evaluate(text)
	if result.Decision == gate.No {
		t.Fatalf("lesson rejected: %+v", result)
	}
}

func TestRejectBlocked(t *testing.T) {
	t.Parallel()
	_, err := gate.RejectBlocked("sk-abcdefghijklmnopqrstuvwxyz0123456789")
	if !errcat.Is(err, errcat.WriteRejected) {
		t.Fatalf("got %v", err)
	}
}
