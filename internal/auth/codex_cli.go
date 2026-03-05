package auth

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func CodexAuthPath(customPath string) (string, error) {
	return resolveCodexAuthPath(customPath)
}

func EnsureCodexLogin(customPath string, force bool, stdout io.Writer, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	authPath, err := resolveCodexAuthPath(customPath)
	if err != nil {
		return err
	}

	if !force {
		if cred, err := LoadCodexCredential(customPath); err == nil && strings.TrimSpace(cred.AccessToken) != "" {
			fmt.Fprintf(stdout, "Codex auth is already available at %s\n", authPath)
			return nil
		}
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH; install it first, then run `codex --login`")
	}

	fmt.Fprintf(stdout, "Starting Codex login using %s\n", codexPath)
	cmd := exec.Command(codexPath, "--login")
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex login failed: %w", err)
	}

	cred, err := LoadCodexCredential(customPath)
	if err != nil {
		return fmt.Errorf("codex login completed but auth validation failed: %w", err)
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return fmt.Errorf("codex login did not produce a usable access token at %s", authPath)
	}

	fmt.Fprintf(stdout, "Codex auth verified at %s\n", authPath)
	return nil
}
