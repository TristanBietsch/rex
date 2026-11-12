//go:build !windows

package main

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/handoff"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexhu")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestRunHandoffCheck(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "probe.json")
	require.NoError(t, handoff.Write(path, handoff.File{Format: handoff.Format, PID: 1, UID: os.Getuid()}))
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	err = runHandoffCheck(path)
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, handoffCheckOK+"\n", string(out))
	require.Error(t, runHandoffCheck(filepath.Join(dir, "missing.json")))
}

func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) //nolint:gosec // G306: test script must be executable.
	return p
}

func TestPreflight(t *testing.T) {
	dir := shortDir(t)
	ctx := context.Background()
	require.NoError(t, preflight(ctx, script(t, dir, "ok", "echo '"+handoffCheckOK+"'"), "probe"))
	err := preflight(ctx, script(t, dir, "old", "echo 'flag provided but not defined: -handoff-check' >&2; exit 2"), "probe")
	require.ErrorContains(t, err, "flag provided but not defined")
	require.ErrorContains(t, err, "-cold")
	err = preflight(ctx, script(t, dir, "silent", "exit 3"), "probe")
	require.ErrorContains(t, err, "exit status 3")
	require.ErrorContains(t, preflight(ctx, script(t, dir, "odd", "echo nope"), "probe"), "unexpected answer")
}

func TestFirstLineAndArgv(t *testing.T) {
	require.Equal(t, "a", firstLine("  a\nb"))
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	require.Len(t, firstLine(string(long)), 200)
	require.Equal(t,
		[]string{os.Args[0], "-socket", "s", "-handoff", "new"},
		handoffArgv([]string{"-socket", "s", "-handoff", "old", "--handoff=x", "-handoff=y"}, "new"))
}

// takeOver adopts the listener and lock named in a handoff file written for
// this pid, marks them close-on-exec, and refuses a file for another socket.
func TestTakeOver(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	ul := l.(*net.UnixListener)
	ul.SetUnlinkOnClose(false)
	lf, err := ul.File()
	require.NoError(t, err)
	_ = l.Close()
	lockF, err := os.Create(filepath.Join(dir, state.LockFileName))
	require.NoError(t, err)
	// takeOver owns what it is handed: give it duplicates, so the test's
	// own *os.File values never close a descriptor a second time.
	lfd, lockFD := dupFD(t, lf), dupFD(t, lockF)
	_ = lf.Close()
	_ = lockF.Close()
	path := handoff.Path(dir, os.Getpid())
	require.NoError(t, handoff.Write(path, handoff.File{Format: handoff.Format, PID: os.Getpid(), UID: os.Getuid(),
		Socket: sock, ListenerFD: lfd, LockFD: lockFD,
		Sessions: []handoff.Session{{ID: "s1", PID: 0, FD: -1}}}))
	tk, err := takeOver(path, dir, sock)
	require.NoError(t, err)
	defer tk.listener.Close()
	defer func() { _ = tk.lock.Release() }()
	require.Equal(t, map[string]bool{"s1": true}, tk.liveIDs())
	c, err := net.Dial("unix", sock)
	require.NoError(t, err)
	_ = c.Close()

	// A session missing from the store, or with no fd/pid, is not adopted.
	require.Empty(t, tk.adoptions(state.NewStore()))

	// Another socket is refused.
	path = handoff.Path(dir, os.Getpid())
	require.NoError(t, handoff.Write(path, handoff.File{Format: handoff.Format, PID: os.Getpid(), UID: os.Getuid(), Socket: "/other"}))
	_, err = takeOver(path, dir, sock)
	require.ErrorContains(t, err, "not")
	require.NoError(t, handoff.Write(path, handoff.File{Format: handoff.Format, PID: os.Getpid(), UID: os.Getuid(), Socket: sock, ListenerFD: -1, LockFD: -1}))
	_, err = takeOver(path, dir, sock)
	require.ErrorContains(t, err, "bad descriptors")
	_, err = takeOver(filepath.Join(dir, "none"), dir, sock)
	require.Error(t, err)

	var nilT *takeover
	require.Nil(t, nilT.liveIDs())
	require.Nil(t, nilT.adoptions(state.NewStore()))
}

// A handed-over session that has no state is stopped: its agent killed.
func TestAdoptionsKillsOrphan(t *testing.T) {
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, child.Start())
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer w.Close()
	tk := &takeover{sessions: map[string]handoff.Session{"gone": {ID: "gone", PID: child.Process.Pid, FD: dupFD(t, r)}}}
	require.Empty(t, tk.adoptions(state.NewStore()))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("orphan not killed")
	}
}

func TestAdoptionsPairsStoredSession(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer w.Close()
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s1", ToolID: "echo"}))
	tk := &takeover{sessions: map[string]handoff.Session{"s1": {ID: "s1", PID: os.Getpid(), FD: dupFD(t, r), Cols: 80, Rows: 24}}}
	got := tk.adoptions(store)
	require.Len(t, got, 1)
	require.Equal(t, uint16(80), got[0].Cols)
	_ = got[0].Adopted.File.Close()
}

// dupFD duplicates f's descriptor for code that takes ownership of it.
func dupFD(t *testing.T, f *os.File) int {
	t.Helper()
	rc, err := f.SyscallConn()
	require.NoError(t, err)
	nfd := -1
	var derr error
	require.NoError(t, rc.Control(func(fd uintptr) { nfd, derr = syscall.Dup(int(fd)) })) //nolint:gosec // G115: descriptors fit an int.
	require.NoError(t, derr)
	syscall.CloseOnExec(nfd)
	return nfd
}
