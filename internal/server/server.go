// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

// Config bundles a Server's dependencies.
type Config struct {
	Socket                string
	StateDir              string
	Registry              *registry.Registry
	Store                 *state.Store
	MaxConcurrentSessions int
}

// Server owns the UDS listener and accepts clients.
type Server struct {
	cfgMu  sync.RWMutex
	cfg    Config
	sem    *semaphore.Weighted // nil = no cap
	wg     sync.WaitGroup
	once   sync.Once //nolint:unused // placeholder for Plan B/C graceful drain
	closed bool      //nolint:unused // placeholder for Plan B/C graceful drain

	inputMu       sync.Mutex
	inputChannels map[string]chan []byte

	outputSubsMu sync.RWMutex
	outputSubs   map[string][]func([]byte) // sessionID -> callbacks
}

// SetRegistry atomically swaps the registry used for future spawns.
// Existing sessions are unaffected.
func (s *Server) SetRegistry(reg *registry.Registry) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.cfg.Registry = reg
}

// Registry returns the current registry (read under lock).
func (s *Server) Registry() *registry.Registry {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Registry
}

// New unlinks any stale socket and constructs a Server. It does not listen yet.
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
	}
	// Unlink any stale socket. If something is actually listening on it we'll
	// fail on Listen below, which is the right outcome.
	_ = os.Remove(cfg.Socket)
	s := &Server{cfg: cfg}
	if cfg.MaxConcurrentSessions > 0 {
		s.sem = semaphore.NewWeighted(int64(cfg.MaxConcurrentSessions))
	}
	return s, nil
}

// Serve listens until ctx is canceled. The socket is unlinked on return.
func (s *Server) Serve(ctx context.Context) error {
	l, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Socket, err)
	}
	defer func() {
		_ = l.Close()
		_ = os.Remove(s.cfg.Socket)
	}()

	// Close the listener when ctx is canceled to unblock Accept.
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			handleClient(ctx, conn, s)
		}()
	}
}

// TryAcquireSession reserves a session slot. Returns false if the cap is reached.
func (s *Server) TryAcquireSession() bool {
	if s.sem == nil {
		return true
	}
	return s.sem.TryAcquire(1)
}

// ReleaseSession returns a session slot.
func (s *Server) ReleaseSession() {
	if s.sem == nil {
		return
	}
	s.sem.Release(1)
}

// RegisterInputChannel attaches a channel for forwarding raw bytes to a session.
// Called by the spawn handler at session creation time.
func (s *Server) RegisterInputChannel(sessionID string, ch chan []byte) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if s.inputChannels == nil {
		s.inputChannels = make(map[string]chan []byte)
	}
	s.inputChannels[sessionID] = ch
}

// UnregisterInputChannel removes the channel after the session exits.
func (s *Server) UnregisterInputChannel(sessionID string) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	delete(s.inputChannels, sessionID)
}

// InputChannel returns the channel for a session, or nil if none registered.
func (s *Server) InputChannel(sessionID string) chan []byte {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	return s.inputChannels[sessionID]
}

// SubscribeSessionOutput registers a per-session output callback. Returns a cancel func.
func (s *Server) SubscribeSessionOutput(sessionID string, fn func([]byte)) func() {
	s.outputSubsMu.Lock()
	defer s.outputSubsMu.Unlock()
	if s.outputSubs == nil {
		s.outputSubs = make(map[string][]func([]byte))
	}
	idx := len(s.outputSubs[sessionID])
	s.outputSubs[sessionID] = append(s.outputSubs[sessionID], fn)
	return func() {
		s.outputSubsMu.Lock()
		defer s.outputSubsMu.Unlock()
		list := s.outputSubs[sessionID]
		if idx < len(list) {
			list[idx] = nil
			s.outputSubs[sessionID] = list
		}
	}
}

// broadcastSessionOutput sends bytes to all subscribers of a session.
func (s *Server) broadcastSessionOutput(sessionID string, b []byte) {
	s.outputSubsMu.RLock()
	defer s.outputSubsMu.RUnlock()
	for _, fn := range s.outputSubs[sessionID] {
		if fn != nil {
			fn(b)
		}
	}
}
