package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func completionTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ZDOTDIR", "")
	if err := os.Unsetenv("ZDOTDIR"); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestZshCompletionRejectsEmptyZdotdir(t *testing.T) {
	home := completionTestHome(t)
	t.Setenv("ZDOTDIR", "")
	rcPath := filepath.Join(home, ".zshrc")
	original := "# existing user configuration\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "zh-CN"} {
		stdout, _, code := runCLI("--lang", lang, "completion", "zsh")
		if code == 0 {
			t.Fatalf("empty exported ZDOTDIR unexpectedly accepted: %s", stdout)
		}
		payload := decodeObject(t, stdout)["error"].(map[string]any)
		if payload["code"] != "CompletionInstallFailed" || !strings.Contains(stdout, "unset ZDOTDIR") {
			t.Fatalf("missing error or recovery command: %s", stdout)
		}
	}
	if got := readCompletionFile(t, rcPath); got != original {
		t.Fatalf("HOME configuration changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".zfunc")); !os.IsNotExist(err) {
		t.Fatalf("script directory created before validating ZDOTDIR: %v", err)
	}
}

func readCompletionFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestZshCompletionInstallsAndIsIdempotent(t *testing.T) {
	home := completionTestHome(t)
	rcPath := filepath.Join(home, ".zshrc")
	original := "# user's config\nexport KEEP_ME=yes" // Keep a missing final newline too.
	if err := os.WriteFile(rcPath, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	var firstRC, firstScript string
	for i, wantStatus := range []string{"installed", "already_installed"} {
		stdout, stderr, code := runCLI("--lang", "en", "completion", "zsh")
		if code != 0 {
			t.Fatalf("install: code=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
		scriptPath := filepath.Join(home, ".zfunc", "_ecctl")
		script := readCompletionFile(t, scriptPath)
		rc := readCompletionFile(t, rcPath)
		result := decodeObject(t, stdout)
		if result["status"] != wantStatus || result["script_path"] != scriptPath || result["config_path"] != rcPath {
			t.Fatalf("installation result = %#v", result)
		}
		if !strings.Contains(script, "#compdef ecctl") || !strings.Contains(script, "__complete") {
			t.Fatal("installed file is not a real dynamic completion script")
		}
		if !strings.HasPrefix(rc, original+"\n") || strings.Count(rc, "# >>> ecctl completion zsh >>>") != 1 {
			t.Fatalf("user config not preserved or duplicate setup: %s", rc)
		}
		if !strings.Contains(rc, "source '") || !strings.Contains(rc, "_ecctl'") {
			t.Fatalf("startup does not load the saved script: %s", rc)
		}
		if reload, _ := result["reload_command"].(string); !strings.HasPrefix(reload, "source '") || !strings.Contains(reload, scriptPath) {
			t.Fatalf("reload command = %q", reload)
		}
		if instructions, _ := result["instructions"].(string); !strings.Contains(instructions, "current session") {
			t.Fatalf("missing current-session instructions: %#v", result)
		}
		if i == 0 {
			firstRC, firstScript = rc, script
		} else if rc != firstRC || script != firstScript {
			t.Fatal("repeated installation changed the files")
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(rcPath)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf(".zshrc permissions changed: info=%v err=%v", info, err)
		}
	}
}

func TestZshCompletionUsesZdotdirAndLoadsInZsh(t *testing.T) {
	home := completionTestHome(t)
	zdir := filepath.Join(home, "zsh space ' quote $dollar `tick`")
	t.Setenv("ZDOTDIR", zdir)
	stdout, stderr, code := runCLI("--lang", "en", "completion", "zsh")
	if code != 0 {
		t.Fatalf("install: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	result := decodeObject(t, stdout)
	if result["config_path"] != filepath.Join(zdir, ".zshrc") {
		t.Fatalf("ZDOTDIR ignored: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("installed into HOME despite ZDOTDIR: %v", err)
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed; filesystem checks passed")
	}
	for _, test := range []struct{ name, script string }{
		{"startup", `print -r -- "${_comps[ecctl]:-missing}"`},
		{"reload", result["reload_command"].(string) + `; print -r -- "${_comps[ecctl]:-missing}"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"-d", "-i", "-c", test.script}
			if test.name == "reload" {
				args = []string{"-f", "-c", test.script}
			}
			cmd := exec.Command(zsh, args...)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "_ecctl" {
				t.Fatalf("zsh did not register completion: %v %s", err, out)
			}
		})
	}
}

func TestZshCompletionUpdatesDescriptionOption(t *testing.T) {
	home := completionTestHome(t)
	for _, args := range [][]string{{"completion", "zsh"}, {"completion", "zsh", "--no-descriptions"}} {
		stdout, stderr, code := runCLI(args...)
		if code != 0 {
			t.Fatalf("install: code=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
	}
	script := readCompletionFile(t, filepath.Join(home, ".zfunc", "_ecctl"))
	if !strings.Contains(script, "__completeNoDesc") {
		t.Fatal("reinstall did not update the generated script")
	}
	if strings.Count(readCompletionFile(t, filepath.Join(home, ".zshrc")), "# >>> ecctl completion zsh >>>") != 1 {
		t.Fatal("reinstall duplicated startup configuration")
	}
}

func TestZshCompletionLocalizesInstallationAndHelp(t *testing.T) {
	completionTestHome(t)
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			stdout, stderr, code := runCLI("--lang", lang, "completion", "zsh")
			if code != 0 {
				t.Fatalf("install: code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			result := decodeObject(t, stdout)
			want := "current session"
			if lang == "zh-CN" {
				want = "当前终端"
			}
			if !strings.Contains(result["instructions"].(string), want) {
				t.Fatalf("installation instructions not localized: %s", stdout)
			}
			for _, args := range [][]string{{"completion", "--help"}, {"completion", "zsh", "--help"}} {
				stdout, stderr, code := runCLI(append([]string{"--lang", lang}, args...)...)
				if code != 0 || !strings.Contains(stdout, ".zshrc") || !strings.Contains(stdout, "ZDOTDIR") || !strings.Contains(stdout, "source") {
					t.Fatalf("incomplete localized help: code=%d stdout=%s stderr=%s", code, stdout, stderr)
				}
			}
		})
	}
}

func TestZshCompletionPreservesSymlinkedRC(t *testing.T) {
	home := completionTestHome(t)
	realPath := filepath.Join(home, "dotfiles", "zshrc")
	if err := os.MkdirAll(filepath.Dir(realPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realPath, []byte("# dotfiles\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rcPath := filepath.Join(home, ".zshrc")
	if err := os.Symlink(realPath, rcPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stdout, stderr, code := runCLI("completion", "zsh")
	if code != 0 {
		t.Fatalf("install: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if target, err := os.Readlink(rcPath); err != nil || target != realPath {
		t.Fatalf("symlink replaced: target=%s err=%v", target, err)
	}
	if got := readCompletionFile(t, realPath); !strings.HasPrefix(got, "# dotfiles\n") || !strings.Contains(got, "source '") {
		t.Fatalf("symlink target not updated: %s", got)
	}
}

func TestZshCompletionRejectsBrokenRCWithoutOverwriting(t *testing.T) {
	for _, original := range []string{
		"# >>> ecctl completion zsh >>>\nuser contents\n",
		"# <<< ecctl completion zsh <<<\nuser contents\n",
	} {
		t.Run(original[:10], func(t *testing.T) {
			home := completionTestHome(t)
			rcPath := filepath.Join(home, ".zshrc")
			if err := os.WriteFile(rcPath, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, _, code := runCLI("--lang", "zh-CN", "completion", "zsh")
			if code == 0 {
				t.Fatalf("broken setup unexpectedly accepted: %s", stdout)
			}
			payload := decodeObject(t, stdout)["error"].(map[string]any)
			if payload["code"] != "CompletionInstallFailed" || payload["message"] != "无法安装 zsh 自动补全" {
				t.Fatalf("unclassified or untranslated failure: %#v", payload)
			}
			if got := readCompletionFile(t, rcPath); got != original {
				t.Fatalf("existing config damaged: %q", got)
			}
			if _, err := os.Stat(filepath.Join(home, ".zfunc", "_ecctl")); !os.IsNotExist(err) {
				t.Fatalf("script written before validating config: %v", err)
			}
		})
	}
}
