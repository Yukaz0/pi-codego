package agent

import (
	"context"
	"sync"
)

type SteeringController struct {
	mu             sync.Mutex
	steeringPrompt string
	followUpQueue  []string
	cancelCurrent  context.CancelFunc
}

func NewSteeringController() *SteeringController {
	return &SteeringController{
		followUpQueue: make([]string, 0),
	}
}

func (s *SteeringController) SetCancelFunc(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCurrent = cancel
}

// Steer interrupts the running turn after the current tool and injects an immediate prompt
func (s *SteeringController) Steer(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steeringPrompt = prompt
	if s.cancelCurrent != nil {
		s.cancelCurrent()
	}
}

// QueueFollowUp queues a prompt to be executed after the current agent turn completes
func (s *SteeringController) QueueFollowUp(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followUpQueue = append(s.followUpQueue, prompt)
}

func (s *SteeringController) PopSteeringPrompt() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.steeringPrompt != "" {
		p := s.steeringPrompt
		s.steeringPrompt = ""
		return p, true
	}
	return "", false
}

func (s *SteeringController) PopFollowUp() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.followUpQueue) > 0 {
		p := s.followUpQueue[0]
		s.followUpQueue = s.followUpQueue[1:]
		return p, true
	}
	return "", false
}
