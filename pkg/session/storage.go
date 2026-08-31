package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pi/pkg/types"
)

type Storage struct {
	SessionDir string
}

func NewStorage(sessionDir string) *Storage {
	if sessionDir == "" {
		home, _ := os.UserHomeDir()
		sessionDir = filepath.Join(home, ".pi", "sessions")
	}
	_ = os.MkdirAll(sessionDir, 0755)
	return &Storage{
		SessionDir: sessionDir,
	}
}

type SessionFileEntry struct {
	Type          string    `json:"type"` // "node", "meta" or "header"
	Name          string    `json:"name,omitempty"`
	Workspace     string    `json:"workspace,omitempty"`
	Model         string    `json:"model,omitempty"`
	ID            string    `json:"id,omitempty"`
	ParentID      string    `json:"parent_id,omitempty"`
	CurrentLeafID string    `json:"current_leaf_id,omitempty"`
	Node          *Node     `json:"node,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

func (s *Storage) SaveSession(sessionID string, tree *Tree) error {
	filePath := filepath.Join(s.SessionDir, fmt.Sprintf("%s.jsonl", sessionID))
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create session file %s: %w", filePath, err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	// Save all nodes
	tree.mu.RLock()
	defer tree.mu.RUnlock()

	for _, node := range tree.Nodes {
		entry := SessionFileEntry{
			Type:      "node",
			ID:        node.ID,
			ParentID:  node.ParentID,
			Node:      node,
			Timestamp: node.CreatedAt,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		_, _ = writer.Write(append(data, '\n'))
	}

	// Write metadata line (current leaf + name)
	meta := SessionFileEntry{
		Type:          "meta",
		Name:          tree.Name,
		CurrentLeafID: tree.CurrentLeafID,
		Timestamp:     time.Now(),
	}
	metaData, _ := json.Marshal(meta)
	_, _ = writer.Write(append(metaData, '\n'))

	return writer.Flush()
}

func (s *Storage) LoadSession(sessionID string) (*Tree, error) {
	filePath := filepath.Join(s.SessionDir, fmt.Sprintf("%s.jsonl", sessionID))
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file %s: %w", filePath, err)
	}
	defer file.Close()

	tree := NewTree()
	scanner := bufio.NewScanner(file)
	const maxLineSize = 10 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry SessionFileEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if entry.Type == "node" && entry.Node != nil {
			tree.Nodes[entry.Node.ID] = entry.Node
			if entry.Node.ParentID == "" {
				tree.RootIDs = append(tree.RootIDs, entry.Node.ID)
			}
		} else if entry.Type == "meta" {
			tree.CurrentLeafID = entry.CurrentLeafID
			if entry.Name != "" {
				tree.Name = entry.Name
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading session file: %w", err)
	}

	return tree, nil
}

// SessionInfo describes a saved session file for listing (resume picker).
type SessionInfo struct {
	ID         string
	Name       string
	FirstMsg   string
	MessageCnt int
	Modified   time.Time
}

// ListSessions returns saved sessions newest-modified first.
func (s *Storage) ListSessions() []SessionInfo {
	entries, err := os.ReadDir(s.SessionDir)
	if err != nil {
		return nil
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info := SessionInfo{ID: id, Modified: fi.ModTime()}
		f, err := os.Open(filepath.Join(s.SessionDir, e.Name()))
		if err != nil {
			out = append(out, info)
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			var entry SessionFileEntry
			if json.Unmarshal(scanner.Bytes(), &entry) != nil {
				continue
			}
			switch entry.Type {
			case "meta":
				if entry.Name != "" {
					info.Name = entry.Name
				}
			case "node":
				info.MessageCnt++
				if entry.Node != nil && info.FirstMsg == "" &&
					entry.Node.Message.Role == types.RoleUser {
					c := strings.ReplaceAll(entry.Node.Message.Content, "\n", " ")
					if len(c) > 60 {
						c = c[:60] + "…"
					}
					info.FirstMsg = c
				}
			}
		}
		f.Close()
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}
