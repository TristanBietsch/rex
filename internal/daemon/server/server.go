// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
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
	// AutoTrustDirs returns the folders whose sessions get their agent's
	// folder-trust dialog accepted (setting auto_trust_dirs). Called at each
	// spawn, so a changed setting applies to the next session. nil: never.
	AutoTrustDirs func() []string
	// SkipPermissions reports whether new sessions get their tool's
	// skip_permissions_args (setting skip_permissions). Called at each
	// spawn. nil: never.
	SkipPermissions func() bool
	// ResumeEnabled reports whether the resume feature is on (setting
	// resume). Called at each spawn and Resume. Off: no conversation id is
	// preassigned or captured, and a Resume intent is refused. nil: on.
	ResumeEnabled func() bool
	// SkillsDir is the skill library (~/.config/rex/skills). NewSession
	// resolves skill names under it; "" means no library.
	SkillsDir string
	// SkillsEnabled reports whether the spawn_skills feature is on. Called
	// at each spawn and Resume. Off: skills are refused at NewSession and no
	// plugin folder is added. nil: on.
	SkillsEnabled func() bool
	// Snapshots is the turn-snapshot store behind Undo and Redo, and
	// SnapshotsEnabled reports whether the turn_snapshots feature is on
	// (called per Undo/Redo). Off or nil: both intents are refused.
	Snapshots        *snapshot.Store
	SnapshotsEnabled func() bool
	// UsageTap returns the statusLine command to add to a Claude session's
	// --settings (the usage status-line tap), or "" for none. Called at each
	// spawn and Resume with the session id. nil: none.
	UsageTap func(sessionID string) string
	// OnUsageSample receives the usage fields a Claude status-line tap sent
	// (the UsageSample intent). Runs on the connection's goroutine and must
	// not block. nil: the intent is rejected as not implemented.
	OnUsageSample func(protocol.UsageSample)
	// UsageQuery answers a UsageQuery intent. An error becomes bad_intent
	// with its message ("usage is off", a bad range). nil: the intent is
	// rejected as not implemented.
	UsageQuery func(protocol.UsageQuery) (protocol.UsageReport, error)
	// Loops serves the loop intents (the loops feature). nil: they are
	// rejected as not implemented. See LoopService.
	Loops LoopService
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
	// Audit receives the audit-trail hooks (nil: no audit trail). Begin runs
	// at spawn with the initial prompt; End runs in the shared teardown path
	// before a session's directory is removed.
	Audit AuditHook
	// OnBoardClient runs after a board viewer (Hello.Client ==
	// protocol.ClientTUI) completed its Hello and was counted. nil: nothing.
	// It runs on the connection's goroutine and must not block.
	OnBoardClient func()
}

// AuditHook is how the server feeds the audit trail (see package audit).
// Implementations are inert while the audit_trail setting is off.
type AuditHook interface {
	// Begin hands over a new session's initial prompt ("" when none) before
	// the session starts. It keeps only what it needs, in memory.
	Begin(sess *state.Session, prompt string)
	// End writes the session's audit record unless it is already written.
	// The server calls it before it removes the session's directory, and
	// keeps the session when it fails.
	End(sess *state.Session) error
}

// resumeOn reports whether the resume feature is on.
func (s *Server) resumeOn() bool { return s.cfg.ResumeEnabled == nil || s.cfg.ResumeEnabled() }

// statusLineFor is the usage tap command for a new or resumed session of
// tool, "" when the tool is not Claude (hooks format) or usage is off.
func (s *Server) statusLineFor(tool registry.Tool, sessionID string) string {
	if s.cfg.UsageTap == nil || tool.Detect.Kind != "hooks" || tool.Detect.Format != "claude" {
		return ""
	}
	return s.cfg.UsageTap(sessionID)
}

// skillsOn reports whether the spawn_skills feature is on.
func (s *Server) skillsOn() bool { return s.cfg.SkillsEnabled == nil || s.cfg.SkillsEnabled() }

// Server owns the UDS listener and accepts clients.
type Server struct {
	// cfg is immutable after New: handlers copy it without locking. Live
	// settings live elsewhere — the concurrency cap in gate, the SIGHUP-swapped
	// registry in reg.
	cfg Config
	reg atomic.Pointer[registry.Registry]
	// tools is the wire form of reg (installed flags, merged models), rebuilt
	// with it so a Hello does not probe PATH.
	tools atomic.Pointer[[]protocol.ToolInfo]
	gate  *concurrencyGate
	// removeSession deletes a session's directory (state.RemoveSession; a test seam).
	removeSession func(root string, s *state.Session) error
	// wg tracks the connection handlers and the retire reaper. Serve waits
	// for it before it waits for sessions, so no handler can start a
	// supervisor (sessions.Add) once sessions.Wait has begun.
	wg sync.WaitGroup
	// sessions tracks the per-session supervisor goroutines (spawn.go).
	sessions sync.WaitGroup
	// supervisorWait bounds how long Serve waits for sessions on shutdown
	// (0 = defaultSupervisorWait).
	supervisorWait time.Duration

	// connsMu guards conns and closing. Serve's shutdown sets closing and
	// aborts every tracked connection; a connection accepted after that is
	// aborted on registration instead of being missed.
	connsMu sync.Mutex
	conns   map[*clientConn]struct{}
	closing bool

	// ownerUID is the only peer uid accept lets through (os.Getuid() from
	// New; -1 on Windows, where verifyPeer does not check).
	ownerUID int

	// handlers maps intent types to their handlers. Built in New and
	// read-only once Serve starts; tests may swap entries before Serve.
	handlers map[string]intentHandler

	// Per-connection outbound limits (0 = defaultClientQueueLen /
	// defaultClientWriteTimeout). Set before Serve; tests shrink them.
	clientQueueLen     int
	clientWriteTimeout time.Duration
	nextConnID         atomic.Uint64
	// maxLineBytes caps one inbound line (0 = protocol.MaxLineBytes).
	maxLineBytes int

	inputMu       sync.Mutex
	inputChannels map[string]chan []byte
	// inputLocks holds one mutex per session (*sync.Mutex), taken around
	// every write of input to its PTY channel so a multi-part send (text,
	// delay, Enter) is never interleaved with another writer's bytes.
	inputLocks sync.Map
	// unitWriters holds each running session's pty.UnitWriter (its single
	// PTY writer). All input goes through it as units; a session without one
	// refuses input ("session input not ready"), it never queues it.
	unitWriters sync.Map
	// writerReady holds, per session whose input channel is registered, a
	// channel closed once its writer has been registered. Input that arrives
	// while the session is still starting waits on it (briefly); input after
	// the writer is gone (the session is exiting) is refused at once.
	writerReady sync.Map
	// writerWait bounds that wait (0 = defaultWriterWait). Tests shrink it.
	writerWait time.Duration

	outputSubsMu sync.RWMutex
	outputSubs   map[string][]func([]byte) // sessionID -> callbacks

	// summarizerHealthSubs is a global fan-out for SummarizerHealth events
	// (not tied to any single session — every connected client subscribes
	// after Hello).
	summarizerHealthMu   sync.RWMutex
	summarizerHealthSubs []func(protocol.SummarizerHealth)

	// overview is the latest board overview (memory only, never persisted),
	// with a global fan-out like SummarizerHealth. overviewMu guards all
	// three.
	overviewMu   sync.RWMutex
	overview     *protocol.Overview
	overviewSubs []func(protocol.Overview)

	// toolsSubs is the global fan-out for ToolsChanged.
	toolsSubsMu sync.RWMutex
	toolsSubs   []func(protocol.ToolsChanged)

	// usage is the latest live usage state (memory only, never persisted),
	// with the same global fan-out as the overview. usageMu guards all three.
	usageMu   sync.RWMutex
	usage     *protocol.Usage
	usageSubs []func(protocol.Usage)

	// loopSubs is the fan-out for Loop events (loops are kept by the
	// service, not here). loopMu guards it.
	loopMu   sync.RWMutex
	loopSubs []func(protocol.LoopSummary)

	// boardClients counts the connections that completed Hello as a board
	// viewer (the rex TUI) and are still open.
	boardClients atomic.Int32

	resizeMu    sync.RWMutex
	resizeFuncs map[string]func(cols, rows uint16) error

	modeMu       sync.RWMutex
	modePreamble map[string]func() []byte

	attachMu    sync.RWMutex
	attachFuncs map[string]func(fn func(repaint func() []byte))

	stopMu    sync.Mutex
	stopFuncs map[string]func()
	// reserved holds the placeholder reservations in stopFuncs (see
	// reserveStop), so unreserveStop and promoteStop can tell their own
	// placeholder from a real stop func.
	reserved map[string]*stopReservation

	completeMu    sync.Mutex
	completeFuncs map[string]func()

	// retireWaitLogged dedups the "expired but busy" log line per session.
	retireWaitMu     sync.Mutex
	retireWaitLogged map[string]bool
}

// SetRegistry atomically swaps the registry used for future spawns.
// Existing sessions are unaffected.
func (s *Server) SetRegistry(reg *registry.Registry) {
	s.storeRegistry(reg)
	s.toolsSubsMu.RLock()
	subs := slices.Clone(s.toolsSubs)
	s.toolsSubsMu.RUnlock()
	if len(subs) == 0 {
		return
	}
	msg := protocol.ToolsChanged{Tools: s.ToolInfos()}
	for _, fn := range subs {
		if fn != nil {
			fn(msg)
		}
	}
}

func (s *Server) storeRegistry(reg *registry.Registry) {
	infos := toolInfos(reg)
	s.reg.Store(reg)
	s.tools.Store(&infos)
}

// SubscribeTools registers a global callback for registry changes and returns
// its cancel func. fn follows the SubscribeOverview contract: no blocking, no
// I/O.
func (s *Server) SubscribeTools(fn func(protocol.ToolsChanged)) func() {
	s.toolsSubsMu.Lock()
	defer s.toolsSubsMu.Unlock()
	idx := len(s.toolsSubs)
	s.toolsSubs = append(s.toolsSubs, fn)
	return func() {
		s.toolsSubsMu.Lock()
		defer s.toolsSubsMu.Unlock()
		if idx < len(s.toolsSubs) {
			s.toolsSubs[idx] = nil
		}
	}
}

// ToolInfos returns the current registry in wire form: whether each binary is
// on PATH (probed when the registry was set) and each tool's models.
func (s *Server) ToolInfos() []protocol.ToolInfo {
	if p := s.tools.Load(); p != nil {
		return *p
	}
	return nil
}

// toolInfos is reg as protocol.ToolInfo.
func toolInfos(reg *registry.Registry) []protocol.ToolInfo {
	if reg == nil {
		return nil
	}
	installed := reg.InstalledMap()
	out := make([]protocol.ToolInfo, len(reg.Tools))
	for i, t := range reg.Tools {
		ti := protocol.ToolInfo{ID: t.ID, Installed: installed[t.ID]}
		for _, m := range t.Models {
			ti.Models = append(ti.Models, protocol.ModelInfo{ID: m.ID, Name: m.Name, Detected: m.Detected})
		}
		out[i] = ti
	}
	return out
}

// Registry returns the current registry.
func (s *Server) Registry() *registry.Registry {
	return s.reg.Load()
}

// New constructs a Server. It touches nothing on disk: Serve claims the
// socket, so a second daemon cannot unlink a live one's socket by merely
// being constructed.
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
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
	s := &Server{
		cfg:           cfg,
		gate:          &concurrencyGate{max: cfg.MaxConcurrentSessions},
		handlers:      defaultHandlers(),
		ownerUID:      os.Getuid(),
		removeSession: state.RemoveSession,
	}
	s.storeRegistry(cfg.Registry)
	// Registry and MaxConcurrentSessions are seeds only; read them through
	// Registry() / MaxConcurrentSessions() so nobody uses a stale copy.
	s.cfg.Registry = nil
	return s, nil
}

// defaultSupervisorWait bounds the shutdown wait for session supervisors:
// long enough for a kill, a PTY drain and a final meta write.
const defaultSupervisorWait = 5 * time.Second

// Serve listens until ctx is canceled or the listener closes (transient
// Accept errors are retried, see acceptConn), then shuts down:
// it closes every client connection, waits for the handlers and the retire
// reaper, and waits (bounded by supervisorWait) for the session supervisors,
// which stop with ctx. Serve may be called at most once per Server.
//
// Serve refuses (ErrSocketInUse) a socket path a live process answers on and
// never replaces a path that is not a socket; a stale socket is replaced. On
// return it unlinks the socket only if the path still names the socket it
// created.
func (s *Server) Serve(ctx context.Context) error {
	l, ours, err := listenSocket(ctx, s.cfg.Socket)
	if err != nil {
		return err
	}
	defer func() {
		_ = l.Close()
		removeOwnSocket(s.cfg.Socket, ours)
	}()

	// serveCtx also ends when Accept fails, so sessions and handlers stop
	// the same way on either exit path.
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Unblock Accept and every handler's Read once serveCtx is done.
	stopClose := context.AfterFunc(serveCtx, func() {
		_ = l.Close()
		s.closeConns()
	})
	defer stopClose()

	// Retire reaper: clears marked-done sessions once their deadline passes.
	// Tracked by wg so shutdown proves it has exited.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runRetireReaper(serveCtx)
	}()

	for {
		conn, err := acceptConn(serveCtx, l)
		if err != nil {
			requested := ctx.Err() != nil
			cancel()
			s.drain()
			if requested {
				return nil
			}
			slog.Error("server: accept failed", "err", err)
			return fmt.Errorf("accept: %w", err)
		}
		if err := verifyPeer(conn, s.ownerUID); err != nil {
			slog.Warn("server: rejected connection", "err", err)
			_ = conn.Close()
			continue
		}
		slog.Debug("server: client connected")
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			handleClient(serveCtx, conn, s)
			slog.Debug("server: client disconnected")
		}()
	}
}

// Backoff between Accept retries after a transient error.
const (
	acceptRetryMin = 5 * time.Millisecond
	acceptRetryMax = time.Second
)

// acceptConn returns the next connection. Accept errors other than the
// listener closing (too many open files, a connection reset before Accept
// took it) are retried with backoff: returning them ended Serve, which
// stopped every running session over one bad moment. It returns an error
// only once the listener is closed or ctx is done.
func acceptConn(ctx context.Context, l net.Listener) (net.Conn, error) {
	delay := time.Duration(0)
	for {
		conn, err := l.Accept()
		if err == nil {
			if delay > 0 {
				slog.Info("server: accept recovered")
			}
			return conn, nil
		}
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			return nil, err
		}
		delay = min(max(2*delay, acceptRetryMin), acceptRetryMax)
		slog.Warn("server: accept failed; retrying", "err", err, "in", delay)
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, err
		}
	}
}

// drain waits for the handlers and the reaper, then (bounded) for the
// session supervisors. Their contexts are already canceled.
func (s *Server) drain() {
	slog.Info("server: shutting down, waiting for clients")
	s.wg.Wait()
	wait := s.supervisorWait
	if wait <= 0 {
		wait = defaultSupervisorWait
	}
	if !waitTimeout(&s.sessions, wait) {
		slog.Warn("server: session supervisors still running; exiting anyway", "waited", wait)
	}
}

// waitTimeout waits for wg for at most d and reports whether it finished.
// On timeout the helper goroutine stays parked in wg.Wait until the group
// finishes; callers use it only on the way out.
func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

// trackConn registers a live connection for shutdown. It returns false once
// shutdown has begun; the caller must then abort the connection itself.
func (s *Server) trackConn(c *clientConn) bool {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.closing {
		return false
	}
	if s.conns == nil {
		s.conns = make(map[*clientConn]struct{})
	}
	s.conns[c] = struct{}{}
	return true
}

// liveConns is the number of tracked client connections.
func (s *Server) liveConns() int {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	return len(s.conns)
}

func (s *Server) untrackConn(c *clientConn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, c)
}

// closeConns starts shutdown: it aborts every live connection, which
// unblocks its handler's Read, and makes trackConn refuse new ones.
func (s *Server) closeConns() {
	s.connsMu.Lock()
	s.closing = true
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.connsMu.Unlock()
	for _, c := range conns {
		c.abort(errServerShutdown)
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
	s.writerReady.Store(sessionID, make(chan struct{}))
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
	s.inputLocks.Delete(sessionID)
	s.unitWriters.Delete(sessionID)
	s.writerReady.Delete(sessionID)
}

// InputChannel returns the channel for a session, or nil if none registered.
func (s *Server) InputChannel(sessionID string) chan []byte {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	return s.inputChannels[sessionID]
}

// SubscribeSessionOutput registers a per-session output callback. Returns a
// cancel func.
//
// Callback contract: fn runs on the session's PTY reader goroutine (the
// supervisor's OutputSink) with outputSubsMu read-held, so a slow fn stalls
// that session's output and its transcript. fn must not block or do I/O
// (client connections encode and enqueue), must not retain b past the call
// unless it copies it, and must not call SubscribeSessionOutput or a cancel
// func. Once the cancel func returns, fn is never called again.
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
// Hello so health flips propagate to all live TUIs. fn runs with
// summarizerHealthMu read-held and follows the SubscribeSessionOutput
// contract: no blocking, no I/O, no (un)subscribing from inside it.
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

// SetOverview stores o as the current overview and sends it to every client
// that has said Hello. The headline is model output, so it is sanitized again
// here (one line, no escapes, at most protocol.MaxOverviewRunes): whatever the
// caller did, no escape sequence reaches a terminal. Nothing is logged or
// persisted.
func (s *Server) SetOverview(o protocol.Overview) {
	o.Headline = protocol.SanitizeDisplayText(o.Headline, protocol.MaxOverviewRunes)
	s.overviewMu.Lock()
	cp := o
	s.overview = &cp
	subs := slices.Clone(s.overviewSubs)
	s.overviewMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(o)
		}
	}
}

// Overview returns a copy of the current overview, nil until SetOverview ran.
func (s *Server) Overview() *protocol.Overview {
	s.overviewMu.RLock()
	defer s.overviewMu.RUnlock()
	if s.overview == nil {
		return nil
	}
	cp := *s.overview
	return &cp
}

// SubscribeOverview registers a global callback for overview changes and
// returns its cancel func. Every connected client subscribes once; fn follows
// the SubscribeSessionOutput contract: no blocking, no I/O.
func (s *Server) SubscribeOverview(fn func(protocol.Overview)) func() {
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	idx := len(s.overviewSubs)
	s.overviewSubs = append(s.overviewSubs, fn)
	return func() {
		s.overviewMu.Lock()
		defer s.overviewMu.Unlock()
		if idx < len(s.overviewSubs) {
			s.overviewSubs[idx] = nil
		}
	}
}

// SetUsage stores u as the current usage state and sends it to every client
// that has said Hello. Nothing is logged or persisted.
func (s *Server) SetUsage(u protocol.Usage) {
	s.usageMu.Lock()
	cp := u
	s.usage = &cp
	subs := slices.Clone(s.usageSubs)
	s.usageMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(u)
		}
	}
}

// Usage returns a copy of the current usage state, nil until SetUsage ran.
func (s *Server) Usage() *protocol.Usage {
	s.usageMu.RLock()
	defer s.usageMu.RUnlock()
	if s.usage == nil {
		return nil
	}
	cp := *s.usage
	cp.Quotas = slices.Clone(cp.Quotas)
	return &cp
}

// SubscribeUsage registers a global callback for usage changes and returns
// its cancel func. fn follows the SubscribeOverview contract: no blocking,
// no I/O.
func (s *Server) SubscribeUsage(fn func(protocol.Usage)) func() {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	idx := len(s.usageSubs)
	s.usageSubs = append(s.usageSubs, fn)
	return func() {
		s.usageMu.Lock()
		defer s.usageMu.Unlock()
		if idx < len(s.usageSubs) {
			s.usageSubs[idx] = nil
		}
	}
}

// BoardClients is how many open connections are board viewers (said Hello as
// protocol.ClientTUI). The overview runner generates only while it is above
// zero.
func (s *Server) BoardClients() int { return int(s.boardClients.Load()) }

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

// RegisterAttach stores a session's attach hook (see
// pty.SupervisorConfig.RegisterAttach). Called by the supervisor at PTY
// start.
func (s *Server) RegisterAttach(sessionID string, fn func(fn func(repaint func() []byte))) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if s.attachFuncs == nil {
		s.attachFuncs = make(map[string]func(fn func(repaint func() []byte)))
	}
	s.attachFuncs[sessionID] = fn
}

// UnregisterAttach removes it after the session exits.
func (s *Server) UnregisterAttach(sessionID string) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	delete(s.attachFuncs, sessionID)
}

// withOutputHeld runs fn with the session's PTY output held (no chunk is
// persisted, broadcast or applied to its screen until fn returns) and
// repaint rendering its current screen. For a session that is not running
// it runs fn at once, and repaint returns nil.
func (s *Server) withOutputHeld(sessionID string, fn func(repaint func() []byte)) {
	s.attachMu.RLock()
	hook := s.attachFuncs[sessionID]
	s.attachMu.RUnlock()
	if hook == nil {
		fn(func() []byte { return nil })
		return
	}
	hook(fn)
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

// stopReservation holds a session's stop slot between the decision to
// respawn it and the supervisor's registration. StopSession on the slot
// (a Delete) cancels the reservation instead of blocking.
type stopReservation struct {
	cancelled atomic.Bool
}

// reserveStop atomically claims id's stop slot with a placeholder. It
// reports false, and changes nothing, when the slot is taken: a live
// supervisor or another resume already holds it. hasStop is true while
// reserved, so a second Resume sees a live session and spawns nothing.
func (s *Server) reserveStop(id string) (*stopReservation, bool) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.stopFuncs[id] != nil {
		return nil, false
	}
	r := &stopReservation{}
	if s.stopFuncs == nil {
		s.stopFuncs = make(map[string]func())
	}
	if s.reserved == nil {
		s.reserved = make(map[string]*stopReservation)
	}
	s.stopFuncs[id] = func() {} // marks the slot taken; StopSession cancels r itself
	s.reserved[id] = r
	return r, true
}

// unreserveStop releases r's placeholder if it still holds the slot (a
// promoted or already-released reservation is left alone).
func (s *Server) unreserveStop(id string, r *stopReservation) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.reserved[id] != r {
		return
	}
	delete(s.reserved, id)
	delete(s.stopFuncs, id)
}

// promoteStop swaps r's placeholder for the real stop func, atomically with
// the cancelled check: it reports false, with the slot released, when a
// Delete cancelled the reservation first. The supervisor must then not start.
func (s *Server) promoteStop(id string, r *stopReservation, stop func()) bool {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.reserved[id] != r {
		return false
	}
	delete(s.reserved, id)
	if r.cancelled.Load() {
		delete(s.stopFuncs, id)
		return false
	}
	s.stopFuncs[id] = stop
	return true
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
	if r := s.reserved[sessionID]; r != nil {
		// A resume holds the slot: cancel it under the lock, so a promote
		// either happened before this (and the real stop is used below) or
		// sees the flag. Calling a copied placeholder after unlocking could
		// land after the swap and be lost.
		r.cancelled.Store(true)
		s.stopMu.Unlock()
		return
	}
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
