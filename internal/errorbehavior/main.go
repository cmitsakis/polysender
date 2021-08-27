package errorbehavior

import (
	"errors"
)

type behavior interface {
	Retryable() bool
}

// IsRetryable returns the retryability of an error.
func IsRetryable(err error) bool {
	var errBehavior behavior
	if errors.As(err, &errBehavior) {
		return errBehavior.Retryable()
	}
	return false
}

type retryable struct {
	Err error
}

func (err retryable) Error() string {
	return err.Err.Error()
}

func (err retryable) Unwrap() error {
	return err.Err
}

func (err retryable) Retryable() bool {
	return true
}

// WrapRetryable marks an error as retryable.
func WrapRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryable{Err: err}
}

type nonRetryable struct {
	Err error
}

func (err nonRetryable) Error() string {
	return err.Err.Error()
}

func (err nonRetryable) Unwrap() error {
	return err.Err
}

func (err nonRetryable) Retryable() bool {
	return false
}

// WrapRetryable marks an error as non-retryable.
func WrapNonRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &nonRetryable{Err: err}
}
