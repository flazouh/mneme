package memory

import "time"

const SchemaVersion = 1

type Type string

const (
	TypePreference     Type = "preference"
	TypeDecision       Type = "decision"
	TypeLesson         Type = "lesson"
	TypeProjectFact    Type = "project_fact"
	TypeIdentity       Type = "identity"
	TypeProcedure      Type = "procedure"
	TypeConstraint     Type = "constraint"
	TypeExplicitMemory Type = "explicit_memory"
)

func (t Type) Valid() bool {
	switch t {
	case TypePreference, TypeDecision, TypeLesson, TypeProjectFact,
		TypeIdentity, TypeProcedure, TypeConstraint, TypeExplicitMemory:
		return true
	default:
		return false
	}
}

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeDomain  Scope = "domain"
	ScopeAgent   Scope = "agent"
	ScopeSession Scope = "session"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopeGlobal, ScopeProject, ScopeDomain, ScopeAgent, ScopeSession:
		return true
	default:
		return false
	}
}

type Lifecycle string

const (
	LifecycleActive      Lifecycle = "active"
	LifecycleArchived    Lifecycle = "archived"
	LifecycleQuarantined Lifecycle = "quarantined"
	LifecycleExpired     Lifecycle = "expired"
	LifecycleSuperseded  Lifecycle = "superseded"
	LifecycleDeleted     Lifecycle = "deleted"
)

func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleActive, LifecycleArchived, LifecycleQuarantined,
		LifecycleExpired, LifecycleSuperseded, LifecycleDeleted:
		return true
	default:
		return false
	}
}

func (l Lifecycle) ActiveRecall() bool {
	return l == LifecycleActive || l == ""
}

type SourceKind string

const (
	SourceExplicitUser          SourceKind = "explicit_user_instruction"
	SourceVerifiedRepositoryFact SourceKind = "verified_repository_fact"
	SourceAgentInference        SourceKind = "agent_inference"
)

func (s SourceKind) Valid() bool {
	switch s {
	case SourceExplicitUser, SourceVerifiedRepositoryFact, SourceAgentInference:
		return true
	default:
		return false
	}
}

func (s SourceKind) Strong() bool {
	return s == SourceExplicitUser || s == SourceVerifiedRepositoryFact
}

type Record struct {
	ID           string     `json:"id"`
	Text         string     `json:"text"`
	Type         Type       `json:"type"`
	Scope        Scope      `json:"scope"`
	Project      string     `json:"project,omitempty"`
	Source       string     `json:"source"`
	SourceKind   SourceKind `json:"source_kind"`
	Lifecycle    Lifecycle  `json:"lifecycle"`
	Importance   int        `json:"importance"`
	Confidence   float64    `json:"confidence"`
	Agent        string     `json:"agent,omitempty"`
	UserID       string     `json:"user_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ValidUntil   *time.Time `json:"valid_until,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`
	Usefulness   int        `json:"usefulness,omitempty"`
}

func (r Record) Recallable(now time.Time) bool {
	if !r.Lifecycle.ActiveRecall() {
		return false
	}
	if r.SupersededBy != "" {
		return false
	}
	if r.ValidUntil != nil && !r.ValidUntil.After(now) {
		return false
	}
	return true
}
