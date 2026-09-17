package main

import (
	"context"

	"golang.org/x/sync/errgroup"
)

const matchPipelineConcurrency = 2

func runMatchPipeline(ctx context.Context, trackIDs []string, task func(context.Context, string) error) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(matchPipelineConcurrency)
	for _, trackID := range trackIDs {
		if err := groupCtx.Err(); err != nil {
			break
		}
		trackID := trackID
		group.Go(func() error { return task(groupCtx, trackID) })
	}
	return group.Wait()
}
