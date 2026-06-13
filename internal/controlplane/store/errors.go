package store

import (
	"errors"
	"fmt"
)

var ErrInvalidInput = errors.New("invalid input")

var ErrServiceNotFound = errors.New("service not found")

func invalidInput(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err)
}
