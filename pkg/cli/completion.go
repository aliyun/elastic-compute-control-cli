package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

const (
	zshCompletionStart  = "# >>> ecctl completion zsh >>>"
	zshCompletionEnd    = "# <<< ecctl completion zsh <<<"
	completionFileLimit = 8 << 20
)

type completionInstallation struct {
	Shell         string `json:"shell"`
	Status        string `json:"status"`
	ScriptPath    string `json:"script_path"`
	ConfigPath    string `json:"config_path"`
	Message       string `json:"message"`
	ReloadCommand string `json:"reload_command"`
	Instructions  string `json:"instructions"`
}

// Keep Cobra's generators for other shells, but make zsh a persistent setup
// command. Dynamic completion still uses Cobra's __complete entry point.
func configureZshCompletion(root *cobra.Command, options *globalOptions, stdout io.Writer) {
	for _, completion := range root.Commands() {
		if completion.Name() != "completion" {
			continue
		}
		localizer := i18n.NewLocalizer(options.lang)
		completion.Short = localizer.CommandShort(completion.CommandPath(), completion.Short)
		for _, shell := range completion.Commands() {
			if shell.Name() == "zsh" {
				completion.RemoveCommand(shell)
				break
			}
		}
		var noDescriptions bool
		cmd := &cobra.Command{
			Use:               "zsh",
			Short:             localizer.Message("CommandShort.ecctl.completion.zsh"),
			Args:              cobra.NoArgs,
			ValidArgsFunction: cobra.NoFileCompletions,
			RunE: func(cmd *cobra.Command, _ []string) error {
				result, err := installZshCompletion(cmd.Context(), cmd.Root(), noDescriptions)
				if err != nil {
					return ecerrors.Client("CompletionInstallFailed", localizer.Message("CompletionInstallFailed"),
						ecerrors.WithDetail(err.Error()),
						ecerrors.WithSuggestion(localizer.Message("SuggestionCompletionInstall")))
				}
				messageID := "CompletionInstalled"
				if result.Status == "already_installed" {
					messageID = "CompletionAlreadyInstalled"
				}
				result.Message = localizer.Message(messageID)
				result.Instructions = localizer.MessageData("CompletionInstructions", map[string]any{"Command": result.ReloadCommand})
				return writeCommandOutput(options, stdout, result)
			},
		}
		cmd.Flags().BoolVar(&noDescriptions, "no-descriptions", false, "disable completion descriptions")
		completion.AddCommand(cmd)
		return
	}
}

func installZshCompletion(ctx context.Context, root *cobra.Command, noDescriptions bool) (completionInstallation, error) {
	result := completionInstallation{Shell: "zsh", Status: "already_installed"}
	zdir, set := os.LookupEnv("ZDOTDIR")
	if set && zdir == "" {
		return result, errors.New("ZDOTDIR is set but empty")
	}
	if !set {
		var err error
		zdir, err = os.UserHomeDir()
		if err != nil {
			return result, err
		}
	}
	zdir, err := filepath.Abs(zdir)
	if err != nil {
		return result, err
	}
	result.ScriptPath = filepath.Join(zdir, ".zfunc", "_ecctl")
	result.ConfigPath = filepath.Join(zdir, ".zshrc")
	result.ReloadCommand = "source " + quoteZshPath(result.ScriptPath)

	var script bytes.Buffer
	// This makes the printed source command work even in a shell that has not
	// enabled completion yet. Keep #compdef first for zsh autoload users too.
	script.WriteString("#compdef ecctl\n\nif (( ! $+functions[compdef] )); then\n  autoload -Uz compinit && compinit || return\nfi\n\n")
	if noDescriptions {
		err = root.GenZshCompletionNoDesc(&script)
	} else {
		err = root.GenZshCompletion(&script)
	}
	if err != nil {
		return result, err
	}
	rcTarget, err := configfile.Resolve(result.ConfigPath, true)
	if err != nil {
		return result, err
	}
	scriptTarget, err := configfile.Resolve(result.ScriptPath, true)
	if err != nil {
		return result, err
	}
	same, err := configfile.SameTarget(rcTarget.Path(), scriptTarget.Path())
	if err != nil {
		return result, err
	}
	if same {
		return result, errors.New("completion script and .zshrc resolve to the same file")
	}
	// Read and replace the resolved targets under their shared locks, preserving
	// symlinks and existing permissions/metadata. Serialize the two-file install
	// using the rc lock; write the script before enabling its loader.
	err = rcTarget.WithLock(ctx, 0, 0, func() error {
		return scriptTarget.WithLock(ctx, 0, 0, func() error {
			rc, rcMode, err := readCompletionTarget(rcTarget)
			if err != nil {
				return err
			}
			block := zshCompletionStart + "\nif [[ -r " + quoteZshPath(result.ScriptPath) + " ]]; then\n  " + result.ReloadCommand + "\nfi\n" + zshCompletionEnd + "\n"
			updatedRC, err := updateZshCompletionRC(string(rc), block)
			if err != nil {
				return fmt.Errorf("update %s: %w", result.ConfigPath, err)
			}
			oldScript, scriptMode, err := readCompletionTarget(scriptTarget)
			if err != nil {
				return err
			}
			if !bytes.Equal(oldScript, script.Bytes()) {
				if err := scriptTarget.AtomicWrite(script.Bytes(), scriptMode); err != nil {
					return fmt.Errorf("write %s: %w", result.ScriptPath, err)
				}
				result.Status = "installed"
			}
			if updatedRC != string(rc) {
				if err := rcTarget.AtomicWrite([]byte(updatedRC), rcMode); err != nil {
					return fmt.Errorf("write %s: %w", result.ConfigPath, err)
				}
				result.Status = "installed"
			}
			return nil
		})
	})
	return result, err
}

func readCompletionTarget(target *configfile.Target) ([]byte, os.FileMode, error) {
	raw, info, err := target.ReadBoundedRegular(completionFileLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", target.Path(), err)
	}
	return raw, info.Mode().Perm(), nil
}

func quoteZshPath(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", "'\\''") + "'"
}

// Replace only complete, marked ecctl blocks. Leave every other byte alone,
// and refuse ambiguous markers rather than risk deleting the user's settings.
func updateZshCompletionRC(content, block string) (string, error) {
	var out strings.Builder
	inside, installed := false, false
	for _, line := range strings.SplitAfter(content, "\n") {
		marker := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch marker {
		case zshCompletionStart:
			if inside {
				return "", errors.New("nested ecctl completion markers")
			}
			if !installed {
				out.WriteString(block)
				installed = true
			}
			inside = true
		case zshCompletionEnd:
			if !inside {
				return "", errors.New("ecctl completion end marker has no start marker")
			}
			inside = false
		default:
			if !inside {
				out.WriteString(line)
			}
		}
	}
	if inside {
		return "", errors.New("ecctl completion start marker has no end marker")
	}
	if !installed {
		if content != "" && !strings.HasSuffix(content, "\n") {
			out.WriteByte('\n')
		}
		out.WriteString(block)
	}
	return out.String(), nil
}
