package memory

import (
	"fmt"
	"strings"

	"github.com/flazouh/mneme/internal/errcat"
)

type WriteInput struct {
	Text       string
	Type       Type
	Scope      Scope
	Project    string
	Source     string
	SourceKind SourceKind
	Importance int
	Confidence float64
	Agent      string
	UserID     string
}

func ValidateWrite(in WriteInput) error {
	if strings.TrimSpace(in.Text) == "" {
		return errcat.New(errcat.InvalidInput, "text is empty")
	}
	if !in.Type.Valid() {
		return errcat.New(errcat.InvalidInput, fmt.Sprintf("unknown type %q", in.Type))
	}
	if !in.Scope.Valid() {
		return errcat.New(errcat.InvalidInput, fmt.Sprintf("unknown scope %q", in.Scope))
	}
	if in.Scope == ScopeProject && strings.TrimSpace(in.Project) == "" {
		return errcat.New(errcat.ProjectUnresolved, "project scope requires a resolved project key")
	}
	if !in.SourceKind.Valid() {
		return errcat.New(errcat.InvalidInput, fmt.Sprintf("unknown source_kind %q", in.SourceKind))
	}
	if strings.TrimSpace(in.Source) == "" {
		return errcat.New(errcat.InvalidInput, "source is empty")
	}
	if in.Importance < 1 || in.Importance > 5 {
		return errcat.New(errcat.InvalidInput, "importance must be 1..5")
	}
	if in.Confidence < 0 || in.Confidence > 1 {
		return errcat.New(errcat.InvalidInput, "confidence must be 0..1")
	}
	highImpact := (in.Type == TypeDecision || in.Type == TypeConstraint) && in.Importance >= 4
	if highImpact && !in.SourceKind.Strong() {
		return errcat.New(errcat.WriteRejected, "high-impact decisions and constraints require explicit_user_instruction or verified_repository_fact source_kind")
	}
	return nil
}
