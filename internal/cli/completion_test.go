package cli

import "testing"

// TestCompletionSubcommand confirms xlq completion <shell> works for
// every shell the README documents (#15) - Cobra only auto-generates
// this command when the root command has a real subcommand of its
// own, which xlq has had since "sheets" was added (ADR-0004). This
// guards against that default silently disappearing, e.g. if
// CompletionOptions.DisableDefaultCmd were ever set, or "sheets"
// were ever removed without anything replacing it as a real
// subcommand.
func TestCompletionSubcommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			out, err := run(t, "completion", shell)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if out == "" {
				t.Error("completion script is empty")
			}
		})
	}
}
