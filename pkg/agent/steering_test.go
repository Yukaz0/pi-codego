package agent

import (
	"sync"
	"testing"
)

func TestSteeringRoundTrip(t *testing.T) {
	s := NewSteeringController()

	if _, ok := s.PopSteeringPrompt(); ok {
		t.Error("empty controller must not pop a steering prompt")
	}

	s.Steer("change topic")
	prompt, ok := s.PopSteeringPrompt()
	if !ok || prompt != "change topic" {
		t.Fatalf("got (%q, %v)", prompt, ok)
	}
	if _, ok := s.PopSteeringPrompt(); ok {
		t.Error("steering prompt should be consumed once")
	}
}

func TestFollowUpQueueFIFO(t *testing.T) {
	s := NewSteeringController()
	s.QueueFollowUp("first")
	s.QueueFollowUp("second")

	p, ok := s.PopFollowUp()
	if !ok || p != "first" {
		t.Errorf("first pop = %q, %v; want 'first'", p, ok)
	}
	p, ok = s.PopFollowUp()
	if !ok || p != "second" {
		t.Errorf("second pop = %q, %v; want 'second'", p, ok)
	}
	if _, ok := s.PopFollowUp(); ok {
		t.Error("queue should be empty")
	}
}

func TestSetCancelFuncIsInvokedBySteer(t *testing.T) {
	s := NewSteeringController()

	called := false
	s.SetCancelFunc(func() { called = true })
	s.Steer("interrupt")

	if !called {
		t.Error("Steer() must cancel the current turn context")
	}
}

// Run with -race to validate the mutex actually protects shared state.
func TestSteeringConcurrentAccess(t *testing.T) {
	s := NewSteeringController()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(4)
		go func(n int) { defer wg.Done(); s.Steer("p") }(i)
		go func(n int) { defer wg.Done(); s.QueueFollowUp("f") }(i)
		go func() { defer wg.Done(); s.PopSteeringPrompt() }()
		go func() { defer wg.Done(); s.PopFollowUp() }()
	}
	wg.Wait()
}
