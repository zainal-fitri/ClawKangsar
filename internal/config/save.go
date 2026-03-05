package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func Save(path string, cfg Config) error {
	cfg.Normalize()

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}

	tempFile, err := os.CreateTemp(dir, "clawkangsar-config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tempPath := tempFile.Name()

	if _, err := tempFile.Write(payload); err != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temp config: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) || fileExists(path) {
			if removeErr := os.Remove(path); removeErr != nil {
				_ = os.Remove(tempPath)
				return fmt.Errorf("remove existing config: %w", removeErr)
			}
			if retryErr := os.Rename(tempPath, path); retryErr == nil {
				return nil
			} else {
				_ = os.Remove(tempPath)
				return fmt.Errorf("replace config: %w", retryErr)
			}
		}
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace config: %w", err)
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
