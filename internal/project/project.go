package project

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/flazouh/mneme/internal/errcat"
)

// Key returns a canonical project identity.
// Explicit values win. Otherwise the git origin remote of cwd is used.
func Key(cwd, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return Normalize(explicit)
	}
	if strings.TrimSpace(cwd) == "" {
		return "", errcat.New(errcat.ProjectUnresolved, "project identity is unresolved; pass --project or run inside a git checkout with origin")
	}
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = cwd
	raw, err := cmd.Output()
	if err != nil {
		return "", errcat.New(errcat.ProjectUnresolved, "project identity is unresolved; pass --project or run inside a git checkout with origin")
	}
	return Normalize(strings.TrimSpace(string(raw)))
}

func Normalize(value string) (string, error) {
	text := strings.TrimSpace(strings.TrimSuffix(value, ".git"))
	if text == "" {
		return "", errcat.New(errcat.InvalidInput, "project key is empty")
	}
	if strings.Contains(text, "://") {
		withoutScheme := text[strings.Index(text, "://")+3:]
		withoutAuth := withoutScheme
		if at := strings.LastIndex(withoutAuth, "@"); at >= 0 {
			withoutAuth = withoutAuth[at+1:]
		}
		text = strings.Trim(withoutAuth, "/")
	} else if colon := strings.Index(text, ":"); colon >= 0 && strings.Contains(text[:colon], "@") {
		host := text[:colon]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		text = host + "/" + strings.Trim(text[colon+1:], "/")
	}
	text = strings.Trim(strings.TrimSuffix(text, ".git"), "/")
	text = strings.Join(strings.Fields(text), "-")
	if text == "" {
		return "", errors.New("project key is empty")
	}
	return strings.ToLower(text), nil
}
