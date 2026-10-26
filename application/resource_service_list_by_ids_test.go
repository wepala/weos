package application

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// idFinderRepo records the one read ListByIDs makes. Any other repository
// method panics through the embedded interface.
type idFinderRepo struct {
	repositories.ResourceRepository

	gotSlug  string
	gotIDs   []string
	gotScope *repositories.VisibilityScope
}

func (r *idFinderRepo) FindAllByTypeAndIDs(
	_ context.Context, typeSlug string, ids []string, scope *repositories.VisibilityScope,
) ([]*entities.Resource, error) {
	r.gotSlug, r.gotIDs, r.gotScope = typeSlug, ids, scope
	return nil, nil
}

// TestResourceService_ListByIDs_ReadsUnderTheCallersScope pins that the read
// by id a flat listing makes for its page's documents (wm-yh1xt5) carries the
// same visibility scope as the listing itself.
func TestResourceService_ListByIDs_ReadsUnderTheCallersScope(t *testing.T) {
	t.Parallel()
	repo := &idFinderRepo{}
	var svc ResourceIDLister = &resourceService{repo: repo, logger: noopLogger{}}
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{AgentID: "agent-1", ActiveAccountID: "acct-1"})
	if _, err := svc.ListByIDs(ctx, "task", []string{"urn:task:1", "urn:task:2"}); err != nil {
		t.Fatal(err)
	}
	if repo.gotSlug != "task" || strings.Join(repo.gotIDs, ",") != "urn:task:1,urn:task:2" {
		t.Errorf("read %q %v, want task [urn:task:1 urn:task:2]", repo.gotSlug, repo.gotIDs)
	}
	want := repositories.VisibilityScope{AgentID: "agent-1", AccountID: "acct-1"}
	if repo.gotScope == nil || *repo.gotScope != want {
		t.Errorf("scope = %+v, want %+v", repo.gotScope, want)
	}
}

// TestResourceService_ListByIDs_RefusesARepositoryThatCannotReadByID pins
// that a repository without the read is an error, not an empty answer.
func TestResourceService_ListByIDs_RefusesARepositoryThatCannotReadByID(t *testing.T) {
	t.Parallel()
	svc := &resourceService{repo: &getFlatStubRepo{}, logger: noopLogger{}}
	if _, err := svc.ListByIDs(context.Background(), "task", []string{"urn:task:1"}); err == nil {
		t.Fatal("expected an error from a repository that cannot read by id")
	}
}
