package errors

import (
	"errors"
	"fmt"
)

type Category string

const (
	Validation   Category = "VALIDATION"
	Unauthorized Category = "UNAUTHORIZED"
	Forbidden    Category = "FORBIDDEN"
	NotFound     Category = "NOT_FOUND"
	Conflict     Category = "CONFLICT"
	Internal     Category = "INTERNAL"
)

type AppError struct {
	Category Category
	Message  string
	Err      error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Category, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Category, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(cat Category, msg string, err error) error {
	return &AppError{
		Category: cat,
		Message:  msg,
		Err:      err,
	}
}

func NewValidation(msg string, err error) error {
	return New(Validation, msg, err)
}

func NewUnauthorized(msg string, err error) error {
	return New(Unauthorized, msg, err)
}

func NewForbidden(msg string, err error) error {
	return New(Forbidden, msg, err)
}

func NewNotFound(msg string, err error) error {
	return New(NotFound, msg, err)
}

func NewConflict(msg string, err error) error {
	return New(Conflict, msg, err)
}

func NewInternal(msg string, err error) error {
	return New(Internal, msg, err)
}

func GetCategory(err error) Category {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Category
	}
	return Internal
}

func GetMessage(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}
