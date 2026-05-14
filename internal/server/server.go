// Package server runs the UDS listener and dispatches per-client handlers.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

// Config bundles a Server's dependencies.
type Config struct {
	Socket   string
	StateDir string
	Registry *registry.Registry
	Store    *state.Store
}

// Server owns the UDS listener and accepts clients.
type Server struct {
	cfg    Config
	wg     sync.WaitGroup
	once   sync.Once //nolint:unused // placeholder for Plan B
	closed bool      //nolint:unused // placeholder for Plan B
}

// New unlinks any stale socket and constructs a Server. It does not listen yet.
func New(cfg Config) (*Server, error) {
	if cfg.Socket == "" {
		return nil, errors.New("server: empty socket path")
	}
	// Unlink any stale socket. If something is actually listening on it we'll
	// fail on Listen below, which is the right outcome.
	_ = os.Remove(cfg.Socket)
	return &Server{cfg: cfg}, nil
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
			handleClient(ctx, conn, s.cfg)
		}()
	}
}
