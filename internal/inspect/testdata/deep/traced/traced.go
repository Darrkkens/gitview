package traced

import (
	"context"
	"runtime"
	"runtime/trace"
	"sync"
	"time"
)

// Pipeline starts workers that exchange values over channels and block on
// each other, inside a user task with regions and a log message.
func Pipeline(ctx context.Context, n int) int {
	ctx, task := trace.NewTask(ctx, "pipeline")
	defer task.End()

	in := make(chan int)
	out := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go worker(ctx, in, out, &wg)
	}
	go func() {
		for i := 0; i < n; i++ {
			in <- i
		}
		close(in)
	}()
	go func() {
		wg.Wait()
		close(out)
	}()
	total := 0
	trace.WithRegion(ctx, "collect", func() {
		for v := range out {
			total += v
		}
	})
	trace.Log(ctx, "result", "collected")
	return total
}

func worker(ctx context.Context, in <-chan int, out chan<- int, wg *sync.WaitGroup) {
	defer wg.Done()
	for v := range in {
		trace.WithRegion(ctx, "square", func() {
			time.Sleep(200 * time.Microsecond)
			out <- Square(v)
		})
	}
}

// Square is instrumented by GitView in tests through an overlay.
func Square(v int) int { return v * v }

// Churn allocates enough to trigger garbage collection.
func Churn() {
	var keep [][]byte
	for i := 0; i < 200; i++ {
		keep = append(keep, make([]byte, 64<<10))
		if len(keep) > 16 {
			keep = keep[1:]
		}
	}
	runtime.GC()
}
