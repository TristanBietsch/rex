package loop

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// helperArgv re-runs the test binary as the check command, so the tests need
// no shell and run the same on every OS. The caller sets REX_LOOP_HELPER=1.
func helperArgv(mode string, args ...string) []string {
	return append([]string{os.Args[0], "-test.run=^TestCheckHelper$", "--", mode}, args...)
}

func TestCheckHelper(t *testing.T) {
	if os.Getenv("REX_LOOP_HELPER") != "1" {
		t.Skip("helper process")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch args[0] {
	case "exit":
		var n int
		_, _ = fmt.Sscan(args[1], &n)
		fmt.Println("out line")
		fmt.Fprintln(os.Stderr, "err line")
		os.Exit(n)
	case "flood":
		chunk := strings.Repeat("x", 1024)
		for range 200 {
			fmt.Print(chunk)
		}
		os.Exit(0)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(99)
}

func TestRunCheck_ExitCodes(t *testing.T) {
	t.Setenv("REX_LOOP_HELPER", "1")
	ok := RunCheck(context.Background(), helperArgv("exit", "0"), t.TempDir(), time.Minute)
	require.True(t, ok.Passed())
	require.Equal(t, 0, ok.ExitCode)
	require.Contains(t, string(ok.Output), "out line")
	require.Contains(t, string(ok.Output), "err line", "stderr is captured too")

	bad := RunCheck(context.Background(), helperArgv("exit", "3"), t.TempDir(), time.Minute)
	require.False(t, bad.Passed())
	require.Equal(t, 3, bad.ExitCode)
	require.NoError(t, bad.Err)
}

func TestRunCheck_StartFailure(t *testing.T) {
	r := RunCheck(context.Background(), []string{"rex-no-such-command-xyz"}, t.TempDir(), time.Minute)
	require.Error(t, r.Err)
	require.False(t, r.Passed())
	r = RunCheck(context.Background(), nil, t.TempDir(), time.Minute)
	require.Error(t, r.Err)
}

func TestRunCheck_OutputCapped(t *testing.T) {
	t.Setenv("REX_LOOP_HELPER", "1")
	r := RunCheck(context.Background(), helperArgv("flood"), t.TempDir(), time.Minute)
	require.True(t, r.Passed())
	require.Len(t, r.Output, MaxCheckOutput)
	require.True(t, r.Truncated)
}

func TestRunCheck_Timeout(t *testing.T) {
	t.Setenv("REX_LOOP_HELPER", "1")
	start := time.Now()
	r := RunCheck(context.Background(), helperArgv("hang"), t.TempDir(), 300*time.Millisecond)
	require.True(t, r.TimedOut)
	require.False(t, r.Passed())
	require.Less(t, time.Since(start), 10*time.Second)
}

func TestRunCheck_ContextCanceled(t *testing.T) {
	t.Setenv("REX_LOOP_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	r := RunCheck(ctx, helperArgv("hang"), t.TempDir(), time.Hour)
	require.False(t, r.TimedOut)
	require.ErrorIs(t, r.Err, context.Canceled)
}

func TestRunCheck_MissingFolderIsAStartFailure(t *testing.T) {
	t.Setenv("REX_LOOP_HELPER", "1")
	dir := t.TempDir()
	require.True(t, RunCheck(context.Background(), helperArgv("exit", "0"), dir, time.Minute).Passed())
	r := RunCheck(context.Background(), helperArgv("exit", "0"), dir+"/missing", time.Minute)
	require.Error(t, r.Err)
}
