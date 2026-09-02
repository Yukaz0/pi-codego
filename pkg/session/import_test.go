package session

import (
	"strings"
	"testing"

	"pi/pkg/types"
)

// Round-trip: a tree's linear history serialized to JSONL (as /export does)
// must survive ReadMessagesJSONL + BuildTreeFromMessages unchanged.
func TestImportRoundTrip(t *testing.T) {
	tree := NewTree()
	tree.AddMessage(types.NewUserMessage("halo"))
	tree.AddMessage(types.NewAssistantMessage("hai", nil))
	tree.AddMessage(types.NewAssistantMessageWithReasoning("jawaban", "pikir: 1+1=2", nil))
	tree.AddMessage(types.NewUserMessage("1+1?"))

	hist := tree.GetLinearHistory("")
	var b strings.Builder
	for _, msg := range hist {
		j, err := msg.ToJSON()
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(j)
		b.WriteString("\n")
	}

	msgs, err := ReadMessagesJSONL(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("ReadMessagesJSONL: %v", err)
	}
	if len(msgs) != len(hist) {
		t.Fatalf("got %d msgs, want %d", len(msgs), len(hist))
	}
	for i := range hist {
		if msgs[i].Role != hist[i].Role || msgs[i].Content != hist[i].Content || msgs[i].Reasoning != hist[i].Reasoning {
			t.Errorf("msg %d mismatch:\n got  %+v\n want %+v", i, msgs[i], hist[i])
		}
	}

	// Rebuilding must produce an identical linear history.
	rebuilt := BuildTreeFromMessages(msgs)
	rehist := rebuilt.GetLinearHistory("")
	if len(rehist) != len(hist) {
		t.Fatalf("rebuilt history len %d, want %d", len(rehist), len(hist))
	}
	for i := range hist {
		if rehist[i].Role != hist[i].Role || rehist[i].Content != hist[i].Content || rehist[i].Reasoning != hist[i].Reasoning {
			t.Errorf("rebuilt msg %d mismatch", i)
		}
	}
}

// SessionFileEntry (node/meta) lines and blanks must be skipped, leaving only
// real flat messages.
func TestImportSkipsNonMessageLines(t *testing.T) {
	j := strings.Join([]string{
		`{"type":"node","id":"a","node":{"id":"a","message":{"role":"user","content":"halo"}}}`,
		"",
		`{"type":"meta","name":"x"}`,
		`not-json`,
		`{"role":"user","content":"valid"}`,
	}, "\n")

	msgs, err := ReadMessagesJSONL(strings.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs, want 1 (only the flat user message)", len(msgs))
	}
	if msgs[0].Content != "valid" {
		t.Errorf("content = %q, want %q", msgs[0].Content, "valid")
	}
}
