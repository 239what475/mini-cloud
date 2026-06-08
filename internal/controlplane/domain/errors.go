package domain

import (
	"errors"
	"fmt"
)

var ErrInvalidInput = errors.New("invalid input")

func InvalidInput(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err)
}

func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}
