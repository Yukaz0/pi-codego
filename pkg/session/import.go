package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"

	"pi/pkg/types"
)

// ReadMessagesJSONL parses a JSONL stream of flat types.Message (one per line),
// the same format written by pi-go /export. Lines that fail to unmarshal, are
// blank, or carry no role (e.g. SessionFileEntry node/meta lines from on-disk
// session files) are skipped so /import round-trips /export cleanly.
func ReadMessagesJSONL(r io.Reader) ([]types.Message, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var out []types.Message
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg types.Message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Role == "" {
			continue // not a flat message (e.g. node/meta entry)
		}
		out = append(out, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// BuildTreeFromMessages reconstructs a linear session tree from a flat list of
// messages, chaining each as a new leaf (root→leaf). Used by /import after a
// ReadMessagesJSONL to restore an exported conversation.
func BuildTreeFromMessages(msgs []types.Message) *Tree {
	tree := NewTree()
	for _, msg := range msgs {
		tree.AddMessage(msg)
	}
	return tree
}
