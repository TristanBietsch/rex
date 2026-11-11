package core

import (
	"bytes"
	"encoding/json"
)

// jsonVersion is the version of the -o json envelope. It changes only on a
// breaking change; adding fields does not change it.
const jsonVersion = 1

// renderJSON returns the versioned document `{"v":1,"cmd":...,"data":...}`
// plus a newline. Payloads come from Result.Data, never from display text.
func renderJSON(cmd string, r Result) (string, error) {
	doc := struct {
		V    int            `json:"v"`
		Cmd  string         `json:"cmd"`
		Data any            `json:"data"`
		Meta map[string]any `json:"meta,omitempty"`
	}{jsonVersion, cmd, r.Data, r.Meta}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", NewExitError(ExitGeneric, "encode json: "+err.Error())
	}
	return buf.String(), nil
}
