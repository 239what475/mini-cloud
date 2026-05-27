package cleanupstack

import (
	"context"
	"errors"
	"time"
)

type Func func(context.Context) error

type Stack struct {
	items []Func
}

func (s *Stack) Push(fn Func) {
	if fn == nil {
		return
	}
	s.items = append(s.items, fn)
}

func (s *Stack) Run(ctx context.Context) error {
	var errs []error
	for i := len(s.items) - 1; i >= 0; i-- {
		if err := s.items[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	s.items = nil
	return errors.Join(errs...)
}

func (s *Stack) RunEach(timeout time.Duration) error {
	var errs []error
	for i := len(s.items) - 1; i >= 0; i-- {
		ctx := context.Background()
		cancel := func() {}
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
		}
		if err := s.items[i](ctx); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	s.items = nil
	return errors.Join(errs...)
}
