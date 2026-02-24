package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Session struct {
	Key      string    `json:"key"`
	Messages []Message `json:"messages"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

type SessionStore struct {
	mu       sync.RWMutex
	storage  string
	sessions map[string]*Session
}

func NewSessionStore(storage string) (*SessionStore, error) {
	store := &SessionStore{
		storage:  strings.TrimSpace(storage),
		sessions: make(map[string]*Session),
	}

	if store.storage == "" {
		return store, nil
	}

	if err := os.MkdirAll(store.storage, 0o755); err != nil {
		return nil, err
	}
	if err := store.loadSessions(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SessionStore) AddMessage(sessionKey string, msg Message) error {
	if s == nil {
		return nil
	}

	key := strings.TrimSpace(sessionKey)
	if key == "" {
		key = "global"
	}

	s.mu.Lock()
	session, exists := s.sessions[key]
	if !exists {
		session = &Session{
			Key:      key,
			Messages: make([]Message, 0, 32),
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		s.sessions[key] = session
	}
	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now()
	s.mu.Unlock()

	return s.Save(key)
}

func (s *SessionStore) History(sessionKey string) []Message {
	if s == nil {
		return nil
	}

	key := strings.TrimSpace(sessionKey)
	if key == "" {
		key = "global"
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[key]
	if !ok {
		return nil
	}

	history := make([]Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

func (s *SessionStore) SessionCount() int {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *SessionStore) TotalMessageCount() int {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, session := range s.sessions {
		total += len(session.Messages)
	}
	return total
}

func (s *SessionStore) Save(sessionKey string) error {
	if s == nil || s.storage == "" {
		return nil
	}

	filename := sanitizeSessionFilename(sessionKey)
	if filename == "." || !filepath.IsLocal(filename) || strings.ContainsAny(filename, `/\`) {
		return os.ErrInvalid
	}

	s.mu.RLock()
	session, ok := s.sessions[sessionKey]
	if !ok {
		s.mu.RUnlock()
		return nil
	}

	snapshot := Session{
		Key:     session.Key,
		Created: session.Created,
		Updated: session.Updated,
	}
	if len(session.Messages) > 0 {
		snapshot.Messages = make([]Message, len(session.Messages))
		copy(snapshot.Messages, session.Messages)
	} else {
		snapshot.Messages = make([]Message, 0)
	}
	s.mu.RUnlock()

	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	targetPath := filepath.Join(s.storage, filename+".json")
	tempFile, err := os.CreateTemp(s.storage, "session-*.tmp")
	if err != nil {
		return err
	}

	tmpPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(0o644); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}

	cleanup = false
	return nil
}

func (s *SessionStore) loadSessions() error {
	entries, err := os.ReadDir(s.storage)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(s.storage, entry.Name())
		payload, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(payload, &session); err != nil {
			continue
		}
		if strings.TrimSpace(session.Key) == "" {
			continue
		}

		s.sessions[session.Key] = &session
	}

	return nil
}

func sanitizeSessionFilename(key string) string {
	return strings.ReplaceAll(strings.TrimSpace(key), ":", "_")
}
