package tui

// bootLine is one rendered row in the splash log.
type bootLine struct {
	Name   string
	Status bootStatus
	Desc   string
	Err    error
}

// bootStatus is the status token for a row.
type bootStatus int

const (
	stepOK bootStatus = iota
	stepFail
	stepWarn
	stepSkip
)
