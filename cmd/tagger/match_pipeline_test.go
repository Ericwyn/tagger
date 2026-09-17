package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunMatchPipelineUsesBoundedConcurrency(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	processed := atomic.Int32{}
	err := runMatchPipeline(context.Background(), []string{"1", "2", "3", "4"}, func(context.Context, string) error {
		current := active.Add(1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		processed.Add(1)
		return nil
	})
	if err != nil || processed.Load() != 4 || maximum.Load() != matchPipelineConcurrency {
		t.Fatalf("processed=%d maximum=%d err=%v", processed.Load(), maximum.Load(), err)
	}
}
