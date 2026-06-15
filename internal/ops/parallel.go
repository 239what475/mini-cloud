package ops

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

func runPlaneTasks(ctx context.Context, planes []Plane, fn func(context.Context, Plane) error) error {
	var wg sync.WaitGroup
	errs := make([]error, len(planes))
	for index, plane := range planes {
		index, plane := index, plane
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx, plane); err != nil {
				errs[index] = fmt.Errorf("plane %s: %w", plane.Name, err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
