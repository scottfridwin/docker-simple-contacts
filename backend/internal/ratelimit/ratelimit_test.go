package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsUpToMaxWithinWindow(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("event %d should be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th event within the window should be rejected")
	}
}

func TestLimiterTracksKeysIndependently(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	if !l.Allow("a") {
		t.Fatal("first event for key a should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("first event for key b should be allowed (independent bucket)")
	}
	if l.Allow("a") {
		t.Fatal("second event for key a should be rejected")
	}
}

func TestLimiterResetsAfterWindowElapses(t *testing.T) {
	l := NewLimiter(1, 10*time.Millisecond)
	if !l.Allow("a") {
		t.Fatal("first event should be allowed")
	}
	if l.Allow("a") {
		t.Fatal("second event before the window elapses should be rejected")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow("a") {
		t.Fatal("event after the window elapses should be allowed again")
	}
}
