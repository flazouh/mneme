package project_test

import (
	"testing"

	"github.com/flazouh/mneme/internal/project"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"git@github.com:flazouh/mneme.git":     "github.com/flazouh/mneme",
		"https://github.com/flazouh/mneme.git": "github.com/flazouh/mneme",
		"https://user:x@github.com/flazouh/mneme": "github.com/flazouh/mneme",
		"GitHub.com/Flazouh/Mneme":             "github.com/flazouh/mneme",
	}
	for in, want := range cases {
		got, err := project.Normalize(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestExplicitKeyWins(t *testing.T) {
	t.Parallel()
	got, err := project.Key("/no/such/cwd", "github.com/flazouh/mneme")
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/flazouh/mneme" {
		t.Fatalf("got %s", got)
	}
}
