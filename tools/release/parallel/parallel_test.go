package parallel_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/parallel"
)

var errOdd = errors.New("odd")

func TestMapKeepsOrderWhenLaterItemsFinishFirst(t *testing.T) {
	items := []int{0, 1, 2, 3}
	finished := make([]chan struct{}, len(items))

	for i := range finished {
		finished[i] = make(chan struct{})
	}

	var order []int

	var mu sync.Mutex

	// Each item waits for the one after it, so they finish last to first.
	results := parallel.Map(t.Context(), len(items), items, func(_ context.Context, i int) (int, error) {
		if i+1 < len(items) {
			<-finished[i+1]
		}

		mu.Lock()

		order = append(order, i)
		mu.Unlock()

		close(finished[i])

		return i * 10, nil
	})

	assert.Equal(t, []int{3, 2, 1, 0}, order)
	assert.Equal(t, []parallel.Result[int]{{Value: 0}, {Value: 10}, {Value: 20}, {Value: 30}}, results)
}

func TestMapKeepsEachItemsError(t *testing.T) {
	results := parallel.Map(t.Context(), 2, []int{1, 2, 3}, func(_ context.Context, i int) (int, error) {
		if i%2 == 1 {
			return i, errOdd
		}

		return i, nil
	})

	require.ErrorIs(t, results[0].Err, errOdd)
	require.NoError(t, results[1].Err)
	require.ErrorIs(t, results[2].Err, errOdd)
	assert.Equal(t, 3, results[2].Value, "a failing item keeps the value fn returned with its error")
}

func TestMapRunsAtMostJobsAtOnce(t *testing.T) {
	const jobs = 2

	var (
		running, peak atomic.Int64
		once          sync.Once
	)

	full := make(chan struct{})

	parallel.Map(t.Context(), jobs, make([]int, 6), func(context.Context, int) (struct{}, error) {
		now := running.Add(1)
		defer running.Add(-1)

		for {
			old := peak.Load()

			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}

		// The first tasks hold their workers until the pool is full, so it
		// fills before any of them ends.
		if now == jobs {
			once.Do(func() { close(full) })
		}

		<-full

		return struct{}{}, nil
	})

	assert.Equal(t, int64(jobs), peak.Load())
}

func TestMapPanicIsThatItemsError(t *testing.T) {
	results := parallel.Map(t.Context(), 2, []string{"a", "b", "c"}, func(_ context.Context, s string) (string, error) {
		if s == "b" {
			panic("boom")
		}

		return s + s, nil
	})

	assert.Equal(t, parallel.Result[string]{Value: "aa"}, results[0])
	assert.Equal(t, parallel.Result[string]{Value: "cc"}, results[2])
	require.ErrorIs(t, results[1].Err, parallel.ErrPanic)
	require.EqualError(t, results[1].Err, "panicked: boom", "the stack stays out of the message")
	assert.Empty(t, results[1].Value)

	panicErr, ok := errors.AsType[*parallel.PanicError](results[1].Err)
	require.True(t, ok)
	assert.Equal(t, "boom", panicErr.Value)
	assert.Contains(t, string(panicErr.Stack), "parallel_test.TestMapPanicIsThatItemsError")
}

func TestMapGoexitIsThatItemsError(t *testing.T) {
	results := parallel.Map(t.Context(), 1, []int{1, 2}, func(_ context.Context, i int) (int, error) {
		if i == 1 {
			runtime.Goexit()
		}

		return i, nil
	})

	assert.Equal(t, parallel.Result[int]{Err: parallel.ErrNoResult}, results[0])
	assert.Equal(t, parallel.Result[int]{Value: 2}, results[1], "the pool keeps running the other items")
}

func TestMapCancelledContextWaitsForRunningTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var finished atomic.Bool

	results := parallel.Map(ctx, 1, []int{1, 2, 3}, func(ctx context.Context, i int) (int, error) {
		cancel()
		<-ctx.Done()
		finished.Store(true)

		return i, nil
	})

	assert.True(t, finished.Load(), "Map returned before the running task finished")
	assert.Equal(t, parallel.Result[int]{Value: 1}, results[0])

	for _, r := range results[1:] {
		require.ErrorIs(t, r.Err, parallel.ErrNotRun)
		require.ErrorIs(t, r.Err, context.Canceled)
		assert.Zero(t, r.Value)
	}
}

func TestMapEmpty(t *testing.T) {
	results := parallel.Map(t.Context(), 1, nil, func(context.Context, int) (int, error) { return 1, nil })

	assert.Empty(t, results)
}

func TestMapJobsBelowOneRunsOneAtATime(t *testing.T) {
	for _, jobs := range []int{0, -3} {
		var running, peak atomic.Int64

		results := parallel.Map(t.Context(), jobs, []int{1, 2, 3}, func(_ context.Context, i int) (int, error) {
			now := running.Add(1)
			defer running.Add(-1)

			for {
				old := peak.Load()

				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}

			runtime.Gosched()

			return i, nil
		})

		assert.Equal(t, []parallel.Result[int]{{Value: 1}, {Value: 2}, {Value: 3}}, results, jobs)
		assert.Equal(t, int64(1), peak.Load(), "jobs %d isn't pond's no limit", jobs)
	}
}
