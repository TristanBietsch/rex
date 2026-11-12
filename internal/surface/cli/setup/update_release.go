package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/selfupdate"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// Update statuses of rex update -check.
const (
	statusCurrent   = "up-to-date"
	statusAvailable = "update-available"
	statusNewer     = "running-newer" // the running build is newer than the release
	statusUnknown   = "unknown"       // dev or pseudo-version: cannot compare
)

// updateCheckData is the `-o json` payload of rex update -check.
type updateCheckData struct {
	Version string `json:"version"`
	Remote  string `json:"remote"`
	Status  string `json:"status"`
}

func (d updateDeps) timeouts() (check, fetch time.Duration) {
	check, fetch = d.checkTimeout, d.fetchTimeout
	if check <= 0 {
		check = selfupdate.DefaultTimeout
	}
	if fetch <= 0 {
		fetch = 10 * time.Minute
	}
	return check, fetch
}

// targetTag is the release to install: -version / REX_VERSION when set,
// else the latest release.
func targetTag(d updateDeps, o updateFlags) (string, error) {
	if o.version != "" {
		tag, err := selfupdate.NormalizeTag(o.version)
		if err != nil {
			return "", core.Errorf(core.ExitInvalidArgs, "rex update -version v2.0.0", "update: %v", err)
		}
		return tag, nil
	}
	check, _ := d.timeouts()
	ctx, cancel := context.WithTimeout(context.Background(), check)
	defer cancel()
	tag, err := d.release.Latest(ctx)
	if err != nil {
		slog.Info("update: latest release lookup failed", "err", err)
		return "", core.Errorf(core.ExitGeneric, "", "update: could not look up the latest release: %v", err)
	}
	return tag, nil
}

// compareStatus says how the running version relates to tag.
func compareStatus(running, tag string) string {
	if !selfupdate.Known(running) {
		return statusUnknown
	}
	c, err := selfupdate.Compare(tag, running)
	switch {
	case err != nil:
		return statusUnknown
	case c > 0:
		return statusAvailable
	case c < 0:
		return statusNewer
	}
	return statusCurrent
}

func runUpdateCheck(p *core.Printer, d updateDeps, o updateFlags) error {
	tag, err := targetTag(d, o)
	if err != nil {
		return err
	}
	data := updateCheckData{Version: d.version, Remote: tag, Status: compareStatus(d.version, tag)}
	slog.Info("update: -check", "version", data.Version, "remote", data.Remote, "status", data.Status)
	return emitUpdateCheck(p, data)
}

func emitUpdateCheck(p *core.Printer, data updateCheckData) error {
	v := core.View{core.KV{Pairs: []core.Pair{
		{Key: "version", Label: "version", Val: core.Cell{Text: data.Version}},
		{Key: "remote", Label: "remote", Val: core.Cell{Text: data.Remote}},
		{Key: "status", Label: "status", Val: core.Cell{Text: data.Status}},
	}}}
	switch data.Status {
	case statusAvailable:
		v = append(v, core.Hint{Cmd: "rex update", Text: "install it"})
	case statusUnknown:
		v = append(v, core.Note{Text: "this build has no release version; rex update -force installs " + data.Remote})
	}
	return p.Emit(core.Result{Cmd: "update", Data: data, View: v})
}

// runReleaseUpdate replaces a release install's binaries with a release
// build from GitHub. It never stops the daemon: the old daemon keeps its
// sessions on the old binary until the user moves it.
func runReleaseUpdate(p *core.Printer, d updateDeps, o updateFlags, exe string) error {
	dir := filepath.Dir(exe)
	if want := binaryName("rex", d.goos); filepath.Base(exe) != want {
		return core.Errorf(core.ExitOperationRefused, installerHint,
			"update: the running binary is %s, not %s; re-run the installer instead", exe, want)
	}
	tag, err := targetTag(d, o)
	if err != nil {
		return err
	}
	status := compareStatus(d.version, tag)
	if !o.force {
		switch status {
		case statusUnknown:
			return core.Errorf(core.ExitOperationRefused, "rex update -force",
				"update: the running rex (%s) has no release version, so rex cannot tell whether %s is newer; use -force to install it", d.version, tag)
		case statusCurrent:
			slog.Info("update: already current", "version", d.version)
			return p.Emit(core.Result{Cmd: "update",
				Data: updateData{From: d.version, To: d.version},
				View: core.ActionView(core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("rex %s is up to date", d.version)},
					core.Field{Key: "from", Val: d.version}, core.Field{Key: "to", Val: d.version})})
		case statusNewer:
			return core.Errorf(core.ExitOperationRefused, fmt.Sprintf("rex update -version %s -force", tag),
				"update: %s is older than the running %s; use -force to downgrade", tag, d.version)
		}
	}

	daemonRunning := d.daemon.running()
	printReleasePlan(p, d.version, tag, dir, daemonRunning)
	if !o.yes {
		ok, err := p.Confirm(fmt.Sprintf("install rex %s?", tag), core.Refusal{Cmd: "update", Flag: "-yes", What: "upgrade",
			Hint: "rex update -yes"})
		if err != nil {
			return err
		}
		if !ok {
			slog.Info("update: user declined")
			return core.Errorf(core.ExitOperationRefused, "", "upgrade cancelled")
		}
	}

	if err := installRelease(p, d, tag, dir); err != nil {
		slog.Info("update: release install failed", "err", err)
		return core.Errorf(core.ExitGeneric, "", "update: %v", err)
	}
	slog.Info("update: complete", "old_version", d.version, "new_version", tag)

	data := updateData{From: d.version, To: tag}
	v := core.ActionView(core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("rex %s → %s", d.version, tag)},
		core.Field{Key: "from", Val: d.version}, core.Field{Key: "to", Val: tag})
	if daemonRunning {
		if d.hasUpgrade != nil && d.hasUpgrade(exe) {
			data.Next = "rex upgrade"
			v = append(v, core.Note{Text: "the running daemon still uses the old binary; `rex upgrade` switches it without ending sessions"},
				core.Hint{Cmd: "rex upgrade", Text: "switch the daemon to the new binary"})
		} else {
			data.Next = "rex daemon restart"
			v = append(v, core.Note{Tone: core.ToneWarn, Text: "the running daemon still uses the old binary; `rex daemon restart` switches it but ends running sessions"},
				core.Hint{Cmd: "rex daemon restart", Text: "switch the daemon when your sessions are done"})
		}
	}
	return p.Emit(core.Result{Cmd: "update", Data: data, View: v})
}

// installRelease downloads, verifies and installs tag into dir.
func installRelease(p *core.Printer, d updateDeps, tag, dir string) error {
	tmp, err := os.MkdirTemp(d.tempDir, "rex-update-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	_, fetch := d.timeouts()
	ctx, cancel := context.WithTimeout(context.Background(), fetch)
	defer cancel()

	p.Progress(fmt.Sprintf("downloading rex %s for %s/%s", tag, d.goos, d.goarch))
	a, err := d.release.Fetch(ctx, tag, d.goos, d.goarch, tmp)
	if err != nil {
		return err
	}
	defer a.Remove()
	p.Progress("checksum ok (" + a.SHA256 + ")")
	st, err := a.Stage(dir)
	if err != nil {
		return err
	}
	p.Progress("installing to " + dir)
	return st.Install()
}

// printReleasePlan shows a release update before asking (stderr, text only).
func printReleasePlan(p *core.Printer, from, to, dir string, daemonRunning bool) {
	if p.Format != core.FormatText {
		return
	}
	daemon := "not running"
	if daemonRunning {
		daemon = "running (left running on the old binary)"
	}
	p.EmitErr(core.View{core.KV{Pairs: []core.Pair{
		{Label: "current", Val: core.Cell{Text: from}},
		{Label: "new", Val: core.Cell{Text: to}},
		{Label: "folder", Val: core.Cell{Text: dir}},
		{Label: "mode", Val: core.Cell{Text: modeLabel(installModeRelease)}},
		{Label: "daemon", Val: core.Cell{Text: daemon}},
	}}})
}

func binaryName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

// hasUpgradeCommand reports whether the rex at exe (just installed) has
// `rex upgrade`, the daemon hot reload: `rex help upgrade` exits 0 only
// for a known command.
func hasUpgradeCommand(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "help", "upgrade")
	cmd.Env = append(os.Environ(), "REX_AGENT=0", "REX_OUTPUT=txt")
	return cmd.Run() == nil
}
