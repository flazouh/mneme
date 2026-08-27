package errcat

import (
	"errors"
	"fmt"
)

// Category is a stable, machine-readable failure class for agents.
type Category string

const (
	WriteRejected      Category = "write_rejected"
	ProjectUnresolved  Category = "project_unresolved"
	NotFound           Category = "not_found"
	EmptyRecall        Category = "empty_recall"
	ServiceUnavailable Category = "service_unavailable"
	InvalidInput       Category = "invalid_input"
	Conflict           Category = "conflict"
)

// Error is an operator-facing failure with a stable category.
type Error struct {
	Cat Category
	Msg string
	Err error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Cat, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Cat, e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Category() Category { return e.Cat }

func New(cat Category, msg string) *Error {
	return &Error{Cat: cat, Msg: msg}
}

func Wrap(cat Category, msg string, err error) *Error {
	return &Error{Cat: cat, Msg: msg, Err: err}
}

func Is(err error, cat Category) bool {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Cat == cat
	}
	return false
}

func CategoryOf(err error) Category {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Cat
	}
	return ""
}
