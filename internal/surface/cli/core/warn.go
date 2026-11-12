package core

import "fmt"

// Warn writes one warning line to stderr; stdout stays the result only. The
// format follows the Printer: `rex: msg` in txt and md, a styled ! line in
// text, `warn cmd=… msg=…` in agent mode and a {"v":1,"warn":…} document in
// json.
func (p *Printer) Warn(msg string) {
	msg = flat(msg)
	var line string
	switch p.Format {
	case FormatAgent:
		fields := []Field{}
		if p.Cmd != "" {
			fields = append(fields, Field{"cmd", p.Cmd})
		}
		line = EncodeLine("warn", append(fields, Field{"msg", msg})...)
	case FormatJSON:
		line = warnJSON(p.Cmd, msg)
	case FormatText:
		line = p.ErrStyles.Warn.Render(p.Glyphs.Warn + " " + msg)
	default:
		line = "rex: " + msg
	}
	_, _ = fmt.Fprintln(p.Err, line)
}

// Progress writes a progress line to stderr in the text format only; every
// other format, and agent mode in particular, stays quiet.
func (p *Printer) Progress(msg string) {
	if p.Format != FormatText {
		return
	}
	_, _ = fmt.Fprintln(p.Err, p.ErrStyles.Dim.Render(p.Glyphs.Info+" "+flat(msg)))
}
