package cmd

import "fmt"

// ExitError requests a specific process exit code without printing an
// additional error message. Commands print their own diagnostics before
// returning it.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}
