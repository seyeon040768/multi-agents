package agent

import "errors"

var (
	ErrNotFound      = errors.New("agent not found")
	ErrAlreadyExists = errors.New("agent already exists")
	ErrConflict      = errors.New("agent was modified by another request")
)

type ValidationError struct{ Code, Message string }

func (e *ValidationError) Error() string { return e.Message }
