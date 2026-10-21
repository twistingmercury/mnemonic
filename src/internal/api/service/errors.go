package service

import "errors"

var (
	// ErrNotFound indicates the requested entity does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates a uniqueness constraint violation (duplicate name).
	ErrConflict = errors.New("conflict")

	// ErrInvalidInput indicates the input failed business rule validation.
	ErrInvalidInput = errors.New("invalid input")

	// ErrServiceUnavailable indicates a backend dependency is unreachable.
	ErrServiceUnavailable = errors.New("service unavailable")
)
