package traced

import (
	"context"
	"testing"
)

func TestPipeline(t *testing.T) {
	if got := Pipeline(context.Background(), 20); got != 2470 {
		t.Fatalf("got %d", got)
	}
	Churn()
}
