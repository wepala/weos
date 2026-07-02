package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
)

type capturingCreator struct {
	cmd CreateResourceCommand
	err error
}

func (c *capturingCreator) Create(
	_ context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	c.cmd = cmd
	return nil, c.err
}

func TestRecordAgentTurn_WritesNote(t *testing.T) {
	creator := &capturingCreator{}
	record := RecordAgentTurn(creator)

	err := record(context.Background(), "conv42", "user1", "who is Ada?", "Ada is an engineer.")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if creator.cmd.TypeSlug != NoteTypeSlug {
		t.Errorf("type slug = %q, want %q", creator.cmd.TypeSlug, NoteTypeSlug)
	}
	var note struct {
		Content string `json:"content"`
		About   string `json:"about"`
	}
	if err := json.Unmarshal(creator.cmd.Data, &note); err != nil {
		t.Fatalf("unmarshal note data: %v", err)
	}
	if !strings.Contains(note.Content, "who is Ada?") || !strings.Contains(note.Content, "Ada is an engineer.") {
		t.Errorf("note content = %q, want both the question and the reply", note.Content)
	}
	if note.About != "urn:agent-conversation:conv42" {
		t.Errorf("note about = %q", note.About)
	}
}

func TestRecordAgentTurn_PropagatesError(t *testing.T) {
	record := RecordAgentTurn(&capturingCreator{err: errors.New("db down")})
	if err := record(context.Background(), "c", "u", "m", "r"); err == nil {
		t.Fatal("expected error to propagate (the orchestrator decides it is non-fatal)")
	}
}
