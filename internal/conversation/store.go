// Package conversation manages per-session message history keyed by
// conversation ID. Entries are evicted after 2 hours of inactivity.
package conversation

import (
	"sync"
	"time"

	"github.com/spencer-weaver/gollama/chat"
)

const ttl = 2 * time.Hour

type entry struct {
	msgs    []chat.Msg
	touched time.Time
}

// Store holds conversation histories in memory, one slice per conversation ID.
type Store struct {
	mu    sync.Mutex
	convs map[string]*entry
}

// NewStore returns an initialised Store and starts a background eviction loop.
func NewStore() *Store {
	s := &Store{convs: make(map[string]*entry)}
	go s.evictLoop()
	return s
}

// Get returns a copy of the message history for convID.
// Returns nil if the conversation does not exist yet.
func (s *Store) Get(id string) []chat.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.convs[id]
	if !ok {
		return nil
	}
	e.touched = time.Now()
	out := make([]chat.Msg, len(e.msgs))
	copy(out, e.msgs)
	return out
}

// Append adds one or more messages to the history for convID.
func (s *Store) Append(id string, msgs ...chat.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.convs[id]
	if !ok {
		e = &entry{}
		s.convs[id] = e
	}
	e.msgs = append(e.msgs, msgs...)
	e.touched = time.Now()
}

func (s *Store) evictLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.evict()
	}
}

func (s *Store) evict() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	for id, e := range s.convs {
		if e.touched.Before(cutoff) {
			delete(s.convs, id)
		}
	}
}
