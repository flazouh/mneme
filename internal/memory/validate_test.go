package memory_test

import (
	"testing"

	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/memory"
)

func TestHighImpactRequiresStrongSource(t *testing.T) {
	t.Parallel()
	err := memory.ValidateWrite(memory.WriteInput{
		Text:       "Decision: always prove tracked files with git ls-tree.",
		Type:       memory.TypeDecision,
		Scope:      memory.ScopeGlobal,
		Source:     "mcp",
		SourceKind: memory.SourceAgentInference,
		Importance: 5,
		Confidence: 0.9,
	})
	if !errcat.Is(err, errcat.WriteRejected) {
		t.Fatalf("got %v", err)
	}
}

func TestHighImpactAllowsExplicitUser(t *testing.T) {
	t.Parallel()
	err := memory.ValidateWrite(memory.WriteInput{
		Text:       "Decision: always prove tracked files with git ls-tree.",
		Type:       memory.TypeDecision,
		Scope:      memory.ScopeGlobal,
		Source:     "mcp",
		SourceKind: memory.SourceExplicitUser,
		Importance: 5,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectScopeRequiresProject(t *testing.T) {
	t.Parallel()
	err := memory.ValidateWrite(memory.WriteInput{
		Text:       "Lesson: check the adapter boundary fixture first.",
		Type:       memory.TypeLesson,
		Scope:      memory.ScopeProject,
		Source:     "cli",
		SourceKind: memory.SourceAgentInference,
		Importance: 3,
		Confidence: 0.8,
	})
	if !errcat.Is(err, errcat.ProjectUnresolved) {
		t.Fatalf("got %v", err)
	}
}
