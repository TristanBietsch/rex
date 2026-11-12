//go:build windows

package audit

const (
	oNoFollow = 0
	oNonBlock = 0
)

func isLinkOpenErr(error) bool { return false }
