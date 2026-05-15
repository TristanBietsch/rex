// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

// concurrencyGate is a live-resizable bounded counter. When max == 0, the gate
// is uncapped. Shrinking max below the current active count is allowed:
// existing sessions keep running, but no new acquires succeed until active
// drops below the new cap.
type concurrencyGate struct {
	mu     sync.Mutex
	active int
	max    int
}

func (g *concurrencyGate) TryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.max > 0 && g.active >= g.max {
		return false
	}
	g.active++
	return true
}

func (g *concurrencyGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active > 0 {
		g.active--
	}
}

func (g *concurrencyGate) SetMax(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.max = n
}

func (g *concurrencyGate) Max() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.max
}

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
	gate   *concurrencyGate
	wg     sync.WaitGroup
	once   sync.Once //nolint:unused // reserved for graceful drain
	closed bool      //nolint:unused // reserved for graceful drain

	inputMu       sync.Mutex
	inputChannels map[string]chan []byte

	outputSubsMu sync.RWMutex
	outputSubs   map[string][]func([]byte) // sessionID -> callbacks

	resizeMu    sync.RWMutex
	resizeFuncs map[string]func(cols, rows uint16) error

	stopMu    sync.Mutex
	stopFuncs map[string]func()
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
	s := &Server{cfg: cfg, gate: &concurrencyGate{max: cfg.MaxConcurrentSessions}}
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
				slog.Info("server: shutting down, waiting for clients")
				s.wg.Wait()
				return nil
			}
			slog.Error("server: accept failed", "err", err)
			return fmt.Errorf("accept: %w", err)
		}
		slog.Debug("server: client connected")
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			handleClient(ctx, conn, s)
			slog.Debug("server: client disconnected")
		}()
	}
}

// TryAcquireSession reserves a session slot. Returns false if the cap is reached.
func (s *Server) TryAcquireSession() bool {
	return s.gate.TryAcquire()
}

// ReleaseSession returns a session slot.
func (s *Server) ReleaseSession() {
	s.gate.Release()
}

// SetMaxConcurrentSessions updates the live concurrency cap. Setting n <= 0
// removes the cap. Existing sessions are not killed.
func (s *Server) SetMaxConcurrentSessions(n int) {
	s.gate.SetMax(n)
	s.cfgMu.Lock()
	s.cfg.MaxConcurrentSessions = n
	s.cfgMu.Unlock()
}

// MaxConcurrentSessions returns the live cap (0 means uncapped).
func (s *Server) MaxConcurrentSessions() int {
	return s.gate.Max()
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

// RegisterResize stores a resize callback for a session. Called by the supervisor at PTY start.
func (s *Server) RegisterResize(sessionID string, fn func(cols, rows uint16) error) {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	if s.resizeFuncs == nil {
		s.resizeFuncs = make(map[string]func(cols, rows uint16) error)
	}
	s.resizeFuncs[sessionID] = fn
}

// UnregisterResize removes the callback after the session exits.
func (s *Server) UnregisterResize(sessionID string) {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	delete(s.resizeFuncs, sessionID)
}

// Resize invokes a session's resize callback if registered.
func (s *Server) Resize(sessionID string, cols, rows uint16) error {
	s.resizeMu.RLock()
	fn := s.resizeFuncs[sessionID]
	s.resizeMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(cols, rows)
}

// TranscriptDir returns the directory backing transcripts (state root + sessions/<id>).
func (s *Server) TranscriptDir() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.StateDir
}

// RegisterStop stores a stop function for a session. handleNewSession registers a
// closure that cancels the supervisor's context and waits for it to exit, so callers
// (e.g. IntentDelete) can synchronously tear down the running PTY.
func (s *Server) RegisterStop(sessionID string, stop func()) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.stopFuncs == nil {
		s.stopFuncs = make(map[string]func())
	}
	s.stopFuncs[sessionID] = stop
}

// UnregisterStop removes the stop func after the supervisor goroutine exits.
func (s *Server) UnregisterStop(sessionID string) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	delete(s.stopFuncs, sessionID)
}

// StopSession invokes the registered stop func (no-op if the session isn't running).
// Blocks until the supervisor goroutine has exited.
func (s *Server) StopSession(sessionID string) {
	s.stopMu.Lock()
	stop := s.stopFuncs[sessionID]
	s.stopMu.Unlock()
	if stop != nil {
		stop()
	}
}
