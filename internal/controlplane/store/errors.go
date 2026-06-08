package store

import (
	"errors"
	"fmt"
)

var ErrInvalidInput = errors.New("invalid input")

func invalidInput(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err)
}
