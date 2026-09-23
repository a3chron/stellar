package cmd

import "github.com/spf13/cobra"

// usageError marks an error as resulting from a genuine argument or flag
// misuse (wrong arg count, unknown/bad flag) rather than a runtime failure
// (offline, theme not found, aborted confirmation, ...). The central error
// printer (printCLIError, in cmd/userError.go) shows the command's usage
// block right after the "Error: ..." line for these, and only these -
// mirroring cobra's own classic behaviour, just driven by this marker
// instead of by toggling SilenceUsage per run.
//
// Error() delegates to the wrapped error so err.Error() (and errors.Is/As
// against it) reads exactly like the original cobra/pflag error - only
// printCLIError ever looks for the usageError wrapper itself.
type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// argsWithUsage wraps a cobra.PositionalArgs validator so a genuine
// argument-count mistake still shows the command's usage - see usageError
// above for how that's now signalled instead of the old
// flip-SilenceUsage-and-let-cobra-print-it approach (which no longer applies
// now that root sets both SilenceErrors and SilenceUsage permanently, and
// prints everything itself in cmd.Execute/printCLIError).
func argsWithUsage(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}
