package main

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	values := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond, 50 * time.Millisecond}
	cases := []struct {
		ratio float64
		want  time.Duration
	}{
		{ratio: 0, want: 10 * time.Millisecond},
		{ratio: 0.50, want: 30 * time.Millisecond},
		{ratio: 0.95, want: 50 * time.Millisecond},
		{ratio: 1, want: 50 * time.Millisecond},
	}
	for _, test := range cases {
		if got := percentile(values, test.ratio); got != test.want {
			t.Fatalf("percentile(%v) = %v, want %v", test.ratio, got, test.want)
		}
	}
}

func TestPercentileEmpty(t *testing.T) {
	if got := percentile(nil, 0.95); got != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", got)
	}
}
