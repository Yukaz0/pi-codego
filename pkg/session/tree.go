package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"pi/pkg/types"
)

type Node struct {
	ID          string        `json:"id"`
	ParentID    string        `json:"parent_id,omitempty"`
	ChildrenIDs []string      `json:"children_ids,omitempty"`
	Message     types.Message `json:"message"`
	CreatedAt   time.Time     `json:"created_at"`
}

type Tree struct {
	mu            sync.RWMutex
	Nodes         map[string]*Node `json:"nodes"`
	RootIDs       []string         `json:"root_ids"`
	CurrentLeafID string           `json:"current_leaf_id"`
	Name          string           `json:"name,omitempty"`
}

func NewTree() *Tree {
	return &Tree{
		Nodes:   make(map[string]*Node),
		RootIDs: make([]string, 0),
	}
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (t *Tree) AddMessage(msg types.Message) *Node {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := msg.ID
	if id == "" {
		id = generateID()
		msg.ID = id
	}

	node := &Node{
		ID:          id,
		ParentID:    t.CurrentLeafID,
		ChildrenIDs: make([]string, 0),
		Message:     msg,
		CreatedAt:   time.Now(),
	}

	t.Nodes[id] = node

	if t.CurrentLeafID == "" {
		t.RootIDs = append(t.RootIDs, id)
	} else if parent, exists := t.Nodes[t.CurrentLeafID]; exists {
		parent.ChildrenIDs = append(parent.ChildrenIDs, id)
	}

	t.CurrentLeafID = id
	return node
}

// GetLinearHistory traverses from current leaf back to root
func (t *Tree) GetLinearHistory(leafID string) []types.Message {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if leafID == "" {
		leafID = t.CurrentLeafID
	}

	var reverseHistory []types.Message
	curr := leafID

	for curr != "" {
		node, exists := t.Nodes[curr]
		if !exists {
			break
		}
		reverseHistory = append(reverseHistory, node.Message)
		curr = node.ParentID
	}

	// Reverse array to obtain chronological order (Root -> Leaf)
	history := make([]types.Message, len(reverseHistory))
	for i, msg := range reverseHistory {
		history[len(reverseHistory)-1-i] = msg
	}

	return history
}

func (t *Tree) SwitchBranch(nodeID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.Nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found in session tree", nodeID)
	}
	t.CurrentLeafID = nodeID
	return nil
}

func (t *Tree) Rewind(steps int) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	curr := t.CurrentLeafID
	for i := 0; i < steps; i++ {
		node, exists := t.Nodes[curr]
		if !exists || node.ParentID == "" {
			break
		}
		curr = node.ParentID
	}

	t.CurrentLeafID = curr
	return curr, nil
}

// SetName sets the session display name.
func (t *Tree) SetName(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Name = name
}

// GetName returns the session display name.
func (t *Tree) GetName() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Name
}

// FindNodeByMessageID returns the node ID holding the given message ID.
func (t *Tree) FindNodeByMessageID(msgID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for id, n := range t.Nodes {
		if n.Message.ID == msgID {
			return id, true
		}
	}
	return "", false
}

// NewBranchFrom switches the current leaf to the node containing message
// msgID, so the next AddMessage forks a branch there (Pi /fork behavior).
func (t *Tree) NewBranchFrom(msgID string) error {
	nodeID, ok := t.FindNodeByMessageID(msgID)
	if !ok {
		return fmt.Errorf("message %s not found in tree", msgID)
	}
	return t.SwitchBranch(nodeID)
}

// CloneLinear returns a NEW tree containing only the current linear path
// (root→leaf) as a single chain — Pi /clone behavior.
func (t *Tree) CloneLinear() *Tree {
	t.mu.RLock()
	msgs := make([]types.Message, 0)
	curr := t.CurrentLeafID
	for curr != "" {
		node, exists := t.Nodes[curr]
		if !exists {
			break
		}
		msgs = append([]types.Message{node.Message}, msgs...)
		curr = node.ParentID
	}
	name := t.Name
	t.mu.RUnlock()

	clone := NewTree()
	clone.Name = name
	for _, m := range msgs {
		mCopy := m
		mCopy.ID = generateID() // fresh IDs so the clone saves independently
		clone.AddMessage(mCopy)
	}
	return clone
}
