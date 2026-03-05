package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	openAIOAuthIssuer   = "https://auth.openai.com"
	openAIOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type CodexAuthFile struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

type CodexCredential struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	ExpiresAt    time.Time
	SourcePath   string
}

func LoadCodexCredential(customPath string) (*CodexCredential, error) {
	path, err := resolveCodexAuthPath(customPath)
	if err != nil {
		return nil, err
	}

	file, err := readCodexAuthFile(path)
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(path)
	expiresAt := time.Now().Add(time.Hour)
	if err == nil {
		expiresAt = stat.ModTime().Add(time.Hour)
	}

	cred := &CodexCredential{
		AccessToken:  file.Tokens.AccessToken,
		RefreshToken: file.Tokens.RefreshToken,
		AccountID:    file.Tokens.AccountID,
		ExpiresAt:    expiresAt,
		SourcePath:   path,
	}

	if strings.TrimSpace(cred.AccessToken) == "" {
		return nil, fmt.Errorf("no access token found in %s; run `codex --login`", path)
	}

	if time.Now().Add(5*time.Minute).Before(cred.ExpiresAt) {
		return cred, nil
	}

	if strings.TrimSpace(cred.RefreshToken) == "" {
		return cred, nil
	}

	refreshed, err := refreshCodexCredential(cred)
	if err != nil {
		return cred, nil
	}
	return refreshed, nil
}

func resolveCodexAuthPath(customPath string) (string, error) {
	if strings.TrimSpace(customPath) != "" {
		return customPath, nil
	}

	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "auth.json"), nil
}

func readCodexAuthFile(path string) (*CodexAuthFile, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file CodexAuthFile
	if err := json.Unmarshal(payload, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &file, nil
}

func refreshCodexCredential(cred *CodexCredential) (*CodexCredential, error) {
	form := url.Values{
		"client_id":     {openAIOAuthClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
		"scope":         {"openid profile email offline_access"},
	}

	resp, err := http.PostForm(openAIOAuthIssuer+"/oauth/token", form)
	if err != nil {
		return nil, fmt.Errorf("refresh codex oauth token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("refresh codex oauth token failed: status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	accountID := extractAccountID(tokenResp.IDToken)
	if accountID == "" {
		accountID = cred.AccountID
	}

	refreshed := &CodexCredential{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountID:    accountID,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		SourcePath:   cred.SourcePath,
	}
	if strings.TrimSpace(refreshed.RefreshToken) == "" {
		refreshed.RefreshToken = cred.RefreshToken
	}

	if err := saveCodexAuthFile(refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func saveCodexAuthFile(cred *CodexCredential) error {
	file := CodexAuthFile{}
	file.Tokens.AccessToken = cred.AccessToken
	file.Tokens.RefreshToken = cred.RefreshToken
	file.Tokens.AccountID = cred.AccountID

	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cred.SourcePath, payload, 0o600)
}

func extractAccountID(token string) string {
	claims, err := parseJWTClaims(token)
	if err != nil {
		return ""
	}
	if accountID, ok := claims["chatgpt_account_id"].(string); ok && accountID != "" {
		return accountID
	}
	if accountID, ok := claims["https://api.openai.com/auth.chatgpt_account_id"].(string); ok && accountID != "" {
		return accountID
	}
	if authClaim, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if accountID, ok := authClaim["chatgpt_account_id"].(string); ok && accountID != "" {
			return accountID
		}
	}
	return ""
}

func parseJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("token is not a jwt")
	}

	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, err
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}
