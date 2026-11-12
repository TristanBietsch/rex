package spawnctx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
// cannot close its own block, and so does any other spelling a model might
// read as a closer ("</PASTED>", "</pasted >"): every "</pasted", in any
// case, gets its slash escaped. A cap passed is an error wrapping ErrTooBig
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
		if err := CheckName(it.Kind, it.Name); err != nil {
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
	var b strings.Builder
	b.WriteString(`<pasted name="` + name + `" lines="` + strconv.Itoa(Lines(body)) + "\">\n")
	if body != "" {
		b.WriteString(escapeClosers(body))
		b.WriteByte('\n')
	}
	b.WriteString("</pasted>")
	return b.String()
}

// closerRE finds what could close a paste block: "</pasted" in any case,
// whatever follows it.
var closerRE = regexp.MustCompile(`(?i)</(pasted)`)

// escapeClosers turns every "</pasted" (any case) into "<\/pasted", so
// "</pasted>" becomes "<\/pasted>".
func escapeClosers(body string) string {
	return closerRE.ReplaceAllString(body, `<\/$1`)
}

// CheckName refuses an item name that would break the prompt or hide
// something in it: empty, an unknown kind, a control rune (a newline would
// start a fake line), a line or paragraph separator (U+2028, U+2029: a
// model may read them as a line break), a bidi or invisible rune Sanitize
// drops, U+FFFD, or a quote or angle bracket in a paste name (it would end
// the name attribute or the tag).
func CheckName(k Kind, name string) error {
	bad := func(why string) error {
		return fmt.Errorf("%s %q: %s: %w", k, name, why, ErrBadItem)
	}
	if k < Paste || k > URL {
		return bad("unknown kind")
	}
	if strings.TrimSpace(name) == "" {
		return bad("empty name")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || isBidi(r) || isInvisible(r) || r == utf8.RuneError {
			return bad("control, separator, bidi, invisible or invalid characters in the name")
		}
	}
	if k == Paste && strings.ContainsAny(name, `"<>`) {
		return bad("quote or angle bracket in a paste name")
	}
	return nil
}
