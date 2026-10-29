// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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
	// SummaryRequest is the channel into the summarizer worker; nil disables AI
	// description generation for this server.
	SummaryRequest chan<- string
	// Now is the clock for retire deadlines (nil = time.Now). Tests inject it.
	Now func() time.Time
	// RetireDefaultTTL applies when a Retire intent carries no ttl (0 = 7 days).
	RetireDefaultTTL time.Duration
	// RetireSweepInterval is how often expired retires are cleared (0 = 2s).
	RetireSweepInterval time.Duration
	// OnShutdown handles a Shutdown intent (nil = intent rejected). The daemon
	// cancels its serve context, same as SIGTERM.
	OnShutdown func()
	// OnReload handles a Reload intent (nil = intent rejected): re-read
	// tools.yaml and swap the registry, same as SIGHUP.
	OnReload func() error
}

// Server owns the UDS listener and accepts clients.
type Server struct {
	// cfg is immutable after New: handlers copy it without locking. Live
	// settings live elsewhere — the concurrency cap in gate, the SIGHUP-swapped
	// registry in reg.
	cfg  Config
	reg  atomic.Pointer[registry.Registry]
	gate *concurrencyGate
	wg   sync.WaitGroup

	inputMu       sync.Mutex
	inputChannels map[string]chan []byte

	outputSubsMu sync.RWMutex
	outputSubs   map[string][]func([]byte) // sessionID -> callbacks

	// summarizerHealthSubs is a global fan-out for SummarizerHealth events
	// (not tied to any single session — every connected client subscribes
	// after Hello).
	summarizerHealthMu   sync.RWMutex
	summarizerHealthSubs []func(protocol.SummarizerHealth)

	resizeMu    sync.RWMutex
	resizeFuncs map[string]func(cols, rows uint16) error

	modeMu       sync.RWMutex
	modePreamble map[string]func() []byte

	stopMu    sync.Mutex
	stopFuncs map[string]func()

	completeMu    sync.Mutex
	completeFuncs map[string]func()

	// retireWaitLogged dedups the "expired but busy" log line per session.
	retireWaitMu     sync.Mutex
	retireWaitLogged map[string]bool

	// stateLock is the exclusive lock on <state-dir>/daemon.lock taken by New
	// and released when Serve returns.
	stateLock *os.File
}

// SetRegistry atomically swaps the registry used for future spawns.
// Existing sessions are unaffected.
func (s *Server) SetRegistry(reg *registry.Registry) {
	s.reg.Store(reg)
}

// Registry returns the current registry.
func (s *Server) Registry() *registry.Registry {
	return s.reg.Load()
}

// New constructs a Server. It refuses (ErrAlreadyRunning) when a daemon
// answers on the socket or holds the state dir's lock, then takes that lock
// and unlinks any stale socket file. It does not listen yet; Serve releases
// the lock when it returns.
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
	}
	lock, err := claimDaemon(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RetireDefaultTTL <= 0 {
		cfg.RetireDefaultTTL = DefaultRetireTTL
	}
	if cfg.RetireSweepInterval <= 0 {
		cfg.RetireSweepInterval = defaultRetireSweepInterval
	}
	s := &Server{cfg: cfg, gate: &concurrencyGate{max: cfg.MaxConcurrentSessions}, stateLock: lock}
	s.reg.Store(cfg.Registry)
	// Registry and MaxConcurrentSessions are seeds only; read them through
	// Registry() / MaxConcurrentSessions() so nobody uses a stale copy.
	s.cfg.Registry = nil
	return s, nil
}

// Serve listens until ctx is canceled. On return the socket is unlinked (if
// it is still ours) and the state-dir lock released.
func (s *Server) Serve(ctx context.Context) error {
	defer releaseStateLock(s.stateLock)
	l, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Socket, err)
	}
	// The listener must not unlink the path on Close: by then a newer daemon
	// may own it. removeOwnSocket checks before unlinking.
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	// Any process that can connect can drive every agent: owner only.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(s.cfg.Socket, 0o600); err != nil {
			_ = l.Close()
			_ = os.Remove(s.cfg.Socket)
			return fmt.Errorf("chmod %s: %w", s.cfg.Socket, err)
		}
	}
	ours, err := os.Lstat(s.cfg.Socket)
	if err != nil {
		slog.Warn("server: stat own socket failed; it will not be unlinked on exit", "socket", s.cfg.Socket, "err", err)
	}
	defer func() {
		_ = l.Close()
		removeOwnSocket(s.cfg.Socket, ours)
	}()

	// Close the listener when ctx is canceled to unblock Accept.
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	// Retire reaper: clears marked-done sessions once their deadline passes.
	// Tracked by wg so shutdown proves it has exited.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runRetireReaper(ctx)
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
	slog.Info("server: max concurrent sessions set", "max", n)
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

// SubscribeSummarizerHealth registers a global callback for SummarizerHealth
// events. Returns a cancel func. Every connected client subscribes once after
// Hello so health flips propagate to all live TUIs.
func (s *Server) SubscribeSummarizerHealth(fn func(protocol.SummarizerHealth)) func() {
	s.summarizerHealthMu.Lock()
	defer s.summarizerHealthMu.Unlock()
	idx := len(s.summarizerHealthSubs)
	s.summarizerHealthSubs = append(s.summarizerHealthSubs, fn)
	return func() {
		s.summarizerHealthMu.Lock()
		defer s.summarizerHealthMu.Unlock()
		if idx < len(s.summarizerHealthSubs) {
			s.summarizerHealthSubs[idx] = nil
		}
	}
}

// BroadcastSummarizerHealth fans out a SummarizerHealth event to every
// connected client. Called by the daemon when the summarizer worker's
// health flag flips.
func (s *Server) BroadcastSummarizerHealth(available bool, reason string) {
	payload := protocol.SummarizerHealth{Available: available, Reason: reason}
	s.summarizerHealthMu.RLock()
	defer s.summarizerHealthMu.RUnlock()
	slog.Debug("server: broadcast SummarizerHealth", "available", available, "reason", reason, "subscribers", len(s.summarizerHealthSubs))
	for _, fn := range s.summarizerHealthSubs {
		if fn != nil {
			fn(payload)
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

// RegisterModePreamble stores a session's mode-preamble builder. Called by
// the supervisor at PTY start.
func (s *Server) RegisterModePreamble(sessionID string, fn func() []byte) {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	if s.modePreamble == nil {
		s.modePreamble = make(map[string]func() []byte)
	}
	s.modePreamble[sessionID] = fn
}

// UnregisterModePreamble removes it after the session exits.
func (s *Server) UnregisterModePreamble(sessionID string) {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	delete(s.modePreamble, sessionID)
}

// ModePreamble returns the escape sequences re-creating the session's current
// terminal modes, or nil when the session isn't running.
func (s *Server) ModePreamble(sessionID string) []byte {
	s.modeMu.RLock()
	fn := s.modePreamble[sessionID]
	s.modeMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// TranscriptDir returns the directory backing transcripts (state root + sessions/<id>).
func (s *Server) TranscriptDir() string {
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

// hasStop reports whether a session has a live supervisor.
func (s *Server) hasStop(sessionID string) bool {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	return s.stopFuncs[sessionID] != nil
}

// dropOutputSubs forgets a deleted session's output subscribers.
func (s *Server) dropOutputSubs(sessionID string) {
	s.outputSubsMu.Lock()
	delete(s.outputSubs, sessionID)
	s.outputSubsMu.Unlock()
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

// RegisterComplete stores a complete-signal closure for a session.
// handleNewSession registers a closure that sends to the supervisor's CompleteCh.
func (s *Server) RegisterComplete(sessionID string, fn func()) {
	s.completeMu.Lock()
	defer s.completeMu.Unlock()
	if s.completeFuncs == nil {
		s.completeFuncs = make(map[string]func())
	}
	s.completeFuncs[sessionID] = fn
}

// UnregisterComplete removes the registered closure after the supervisor exits.
func (s *Server) UnregisterComplete(sessionID string) {
	s.completeMu.Lock()
	defer s.completeMu.Unlock()
	delete(s.completeFuncs, sessionID)
}

// CompleteSession invokes the registered complete closure (no-op if absent).
// Unlike StopSession, this does NOT block on supervisor exit — the closure
// is a non-blocking send to a buffered channel.
func (s *Server) CompleteSession(sessionID string) {
	s.completeMu.Lock()
	fn := s.completeFuncs[sessionID]
	s.completeMu.Unlock()
	if fn != nil {
		fn()
	}
}
