package session

import (
	"testing"

	"pi/pkg/types"
)

func TestAddMessageLinearHistory(t *testing.T) {
	tree := NewTree()
	u := tree.AddMessage(types.NewUserMessage("hi"))
	a := tree.AddMessage(types.NewAssistantMessage("halo", nil))

	if u.ParentID != "" {
		t.Errorf("first node should be root, got parent %q", u.ParentID)
	}
	if a.ParentID != u.ID {
		t.Errorf("assistant parent should be %q, got %q", u.ID, a.ParentID)
	}

	hist := tree.GetLinearHistory("")
	if len(hist) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(hist))
	}
	if hist[0].Role != types.RoleUser || hist[0].Content != "hi" {
		t.Errorf("hist[0] = %+v, want user 'hi'", hist[0])
	}
	if hist[1].Role != types.RoleAssistant || hist[1].Content != "halo" {
		t.Errorf("hist[1] = %+v, want assistant 'halo'", hist[1])
	}

	// Explicit leaf ID must behave the same.
	hist2 := tree.GetLinearHistory(a.ID)
	if len(hist2) != 2 || hist2[0].Content != "hi" {
		t.Errorf("GetLinearHistory(leaf) mismatch: %+v", hist2)
	}
}

func TestSwitchBranchCreatesBranch(t *testing.T) {
	tree := NewTree()
	n1 := tree.AddMessage(types.NewUserMessage("branch A start"))
	tree.AddMessage(types.NewUserMessage("A continuation"))

	// Switch back and fork an alternative branch.
	if err := tree.SwitchBranch(n1.ID); err != nil {
		t.Fatalf("SwitchBranch: %v", err)
	}
	tree.AddMessage(types.NewUserMessage("B continuation"))

	hist := tree.GetLinearHistory("")
	want := []string{"branch A start", "B continuation"}
	if len(hist) != len(want) {
		t.Fatalf("history len = %d, want %d (%+v)", len(hist), len(want), hist)
	}
	for i, w := range want {
		if hist[i].Content != w {
			t.Errorf("hist[%d].Content = %q, want %q", i, hist[i].Content, w)
		}
	}

	// Old branch still reachable by leaf ID.
	oldHist := tree.GetLinearHistory(tree.Nodes[n1.ID].ChildrenIDs[0])
	if len(oldHist) != 2 || oldHist[1].Content != "A continuation" {
		t.Errorf("old branch lost: %+v", oldHist)
	}
	if len(tree.RootIDs) != 1 {
		t.Errorf("RootIDs = %v, want single root", tree.RootIDs)
	}
}

func TestRewind(t *testing.T) {
	tree := NewTree()
	n1 := tree.AddMessage(types.NewUserMessage("m1"))
	n2 := tree.AddMessage(types.NewAssistantMessage("m2", nil))
	n3 := tree.AddMessage(types.NewUserMessage("m3"))

	leaf, err := tree.Rewind(1)
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if leaf != n2.ID || tree.CurrentLeafID != n2.ID {
		t.Errorf("after rewind leaf = %q, want %q", tree.CurrentLeafID, n2.ID)
	}

	// Rewinding further than the root clamps at root.
	leaf, _ = tree.Rewind(99)
	if leaf != n1.ID {
		t.Errorf("clamped leaf = %q, want root %q", leaf, n1.ID)
	}

	// Adding after rewind extends from the rewound point.
	tree.AddMessage(types.NewUserMessage("retry"))
	hist := tree.GetLinearHistory("")
	if len(hist) != 2 || hist[1].Content != "retry" {
		t.Errorf("post-rewind history = %+v", hist)
	}
	if n3.ChildrenIDs == nil {
		t.Error("original node n3 was mutated")
	}
}

func TestSwitchBranchUnknownNode(t *testing.T) {
	tree := NewTree()
	if err := tree.SwitchBranch("nonexistent"); err == nil {
		t.Fatal("expected error for unknown node")
	}
}
