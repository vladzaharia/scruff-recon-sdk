package core

import (
	"context"
	"errors"
)

// ErrStopPagination ends a Paginate loop early from the yield function without
// being reported as an error.
var ErrStopPagination = errors.New("core: stop pagination")

// PageFunc fetches one page given a cursor, returning the items and the cursor
// for the next page. done reports that this was the last page.
//
// The cursor is opaque and network-specific: Recon pages message history with
// the ISO timestamp of the oldest message it holds, SCRUFF with the lowest
// message version. Neither uses an offset, which is why this is generic over
// the cursor type rather than taking a page number.
type PageFunc[T, C any] func(ctx context.Context, cursor C) (items []T, next C, done bool, err error)

// Paginate walks pages, calling yield for each item.
//
// It stops when a page reports done, when a page comes back empty, or when
// yield returns an error. ErrStopPagination stops without being reported.
//
// maxItems caps the total yielded; zero means unlimited. A cap is worth setting
// against endpoints that are not paginated at all — Recon's conversation list
// returned 873 records in one response, and its profile search returns the
// entire result set.
func Paginate[T, C any](ctx context.Context, start C, maxItems int, page PageFunc[T, C], yield func(T) error) error {
	cursor, n := start, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, next, done, err := page(ctx, cursor)
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := yield(it); err != nil {
				if errors.Is(err, ErrStopPagination) {
					return nil
				}
				return err
			}
			n++
			if maxItems > 0 && n >= maxItems {
				return nil
			}
		}
		if done || len(items) == 0 {
			return nil
		}
		cursor = next
	}
}

// Collect is Paginate accumulating into a slice.
func Collect[T, C any](ctx context.Context, start C, maxItems int, page PageFunc[T, C]) ([]T, error) {
	var out []T
	err := Paginate(ctx, start, maxItems, page, func(t T) error {
		out = append(out, t)
		return nil
	})
	return out, err
}
