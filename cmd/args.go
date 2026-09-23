package cmd

import "github.com/spf13/cobra"

// argsWithUsage wraps a cobra.PositionalArgs validator so a genuine
// argument-count mistake still prints the command's usage, even though the
// root command sets SilenceUsage. SilenceUsage exists so a runtime error
// (offline, theme not found, aborted confirmation, ...) doesn't dump the
// whole usage block after it - but "you typed the wrong number of
// arguments" is exactly the kind of mistake usage output is for, so commands
// that take positional arguments wrap their validator with this instead of
// passing e.g. cobra.ExactArgs(1) directly.
//
// Rather than printing usage here (which would show it BEFORE cobra's own
// "Error: ..." line once Execute() returns - the reverse of cobra's
// classic order), this flips the root command's SilenceUsage off for the
// rest of this run. Cobra's own post-Execute logic then prints "Error: ..."
// followed by the usage block itself, in that order, exactly as it always
// did before SilenceUsage was introduced on root. Root's flag only ever
// matters for the one command that just ran, so mutating it here is safe.
func argsWithUsage(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			cmd.Root().SilenceUsage = false
			return err
		}
		return nil
	}
}
