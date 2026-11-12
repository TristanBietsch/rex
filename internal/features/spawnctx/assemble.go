package spawnctx

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// contextHeader opens the context part of the prompt.
const contextHeader = "Context from rex (read before starting):"

// Assemble builds a new session's InitialPrompt: the task, then one line per
// reference, then each paste in a <pasted> block, in item order within each
// part:
//
//	<task>
//
//	Context from rex (read before starting):
//	- file: <path>
//	- dir: <path>/ (<n> files)
//	- url: <url>
//
//	<pasted name="paste N" lines="L">
//	<body>
//	</pasted>
//
// With no items the result is the task alone. The task and paste bodies go
// through Sanitize; a "</pasted>" inside a body becomes "<\/pasted>" so it
// cannot close its own block. A cap passed is an error wrapping ErrTooBig
// that names the item and the sizes; a name that would break the prompt is
// an error wrapping ErrBadItem.
func Assemble(task string, items []Item, c Caps) (string, error) {
	if c.Items > 0 && len(items) > c.Items {
		return "", fmt.Errorf("%d context items; the limit is %d: %w", len(items), c.Items, ErrTooBig)
	}
	task = strings.TrimRightFunc(Sanitize(task), unicode.IsSpace)
	if strings.TrimSpace(task) == "" {
		task = ""
	}

	var refs, pastes []string
	pasteTotal := 0
	for _, it := range items {
		if err := checkName(it); err != nil {
			return "", err
		}
		switch it.Kind {
		case File:
			refs = append(refs, "- file: "+it.Name)
		case Dir:
			refs = append(refs, fmt.Sprintf("- dir: %s/ (%d files)", strings.TrimSuffix(it.Name, "/"), it.Files))
		case URL:
			refs = append(refs, "- url: "+it.Name)
		case Paste:
			body := strings.TrimRight(Sanitize(it.Text), "\n")
			if c.Paste > 0 && len(body) > c.Paste {
				return "", fmt.Errorf("%s is %d bytes; the limit is %d: %w", it.Name, len(body), c.Paste, ErrTooBig)
			}
			pasteTotal += len(body)
			if c.PasteTotal > 0 && pasteTotal > c.PasteTotal {
				return "", fmt.Errorf("pastes are %d bytes in all at %s; the limit is %d: %w", pasteTotal, it.Name, c.PasteTotal, ErrTooBig)
			}
			pastes = append(pastes, pasteBlock(it.Name, body))
		}
	}

	var parts []string
	if task != "" {
		parts = append(parts, task)
	}
	if len(refs) > 0 {
		parts = append(parts, contextHeader+"\n"+strings.Join(refs, "\n"))
	}
	parts = append(parts, pastes...)
	prompt := strings.Join(parts, "\n\n")
	if c.Prompt > 0 && len(prompt) > c.Prompt {
		return "", fmt.Errorf("the prompt is %d bytes; the limit is %d: %w", len(prompt), c.Prompt, ErrTooBig)
	}
	return prompt, nil
}

// pasteBlock wraps a sanitized body (no trailing newline) in its tags.
func pasteBlock(name, body string) string {
	lines := 0
	if body != "" {
		lines = strings.Count(body, "\n") + 1
	}
	var b strings.Builder
	b.WriteString(`<pasted name="` + name + `" lines="` + strconv.Itoa(lines) + "\">\n")
	if body != "" {
		b.WriteString(strings.ReplaceAll(body, "</pasted>", `<\/pasted>`))
		b.WriteByte('\n')
	}
	b.WriteString("</pasted>")
	return b.String()
}

// checkName refuses an item whose name would break the prompt: empty, an
// unknown kind, a control or bidi rune (a newline would start a fake line),
// or a quote in a paste name (it would end the name attribute).
func checkName(it Item) error {
	bad := func(why string) error {
		return fmt.Errorf("%s %q: %s: %w", it.Kind, it.Name, why, ErrBadItem)
	}
	if it.Kind < Paste || it.Kind > URL {
		return bad("unknown kind")
	}
	if strings.TrimSpace(it.Name) == "" {
		return bad("empty name")
	}
	for _, r := range it.Name {
		if unicode.IsControl(r) || isBidi(r) || r == '�' {
			return bad("control, bidi or invalid characters in the name")
		}
	}
	if it.Kind == Paste && strings.ContainsAny(it.Name, `"<>`) {
		return bad("quote or angle bracket in a paste name")
	}
	return nil
}
