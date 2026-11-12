//go:build windows

package audit

import "errors"

func syscallMkfifo(string) error { return errors.New("no fifo on windows") }
