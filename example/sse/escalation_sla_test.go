package main

import (
	"testing"
	"time"
)

func TestSLADurationForPriority(t *testing.T) {
	tests := []struct {
		priority string
		want     time.Duration
	}{
		{priority: "P1", want: 10 * time.Minute},
		{priority: "P2", want: 30 * time.Minute},
		{priority: "P3", want: 2 * time.Hour},
		{priority: "", want: 2 * time.Hour},
	}
	for _, test := range tests {
		if got := slaDurationForPriority(test.priority); got != test.want {
			t.Fatalf("slaDurationForPriority(%q) = %s, want %s", test.priority, got, test.want)
		}
	}
}

func TestSLADeadlineUsesCreatedAt(t *testing.T) {
	createdAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.Local)
	if got := slaDeadline(createdAt, "P1"); !got.Equal(createdAt.Add(10 * time.Minute)) {
		t.Fatalf("slaDeadline() = %s", got)
	}
}
