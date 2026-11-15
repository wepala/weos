// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/infrastructure/models"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// Copilot 4580547776: after an account adoption, a reproject replays each
// resource's creation in the source account and its move to the target.
// When two people created the resources — one wrote the course, the other
// wrote a course instance naming it and reads the course through a read
// grant — the instance's course label must survive the reproject. On the
// second pass the course's projection row is already in the target while its
// replayed canonical record is back in the source, so the label resolves
// only through the canonical fallback, and that fallback must honor the
// read grant the projection lookup honors.
func TestReproject_AfterAnAdoptionKeepsTheLabelOfAReferenceReadThroughAGrant(t *testing.T) {
	env := newReprojectTestEnv(t)
	ctx := context.Background()
	if err := env.db.AutoMigrate(&models.ResourcePermission{}); err != nil {
		t.Fatal(err)
	}

	pos := map[string]int{}
	appendEvt := func(aggregateID, eventType string, payload any, txID string) {
		t.Helper()
		pos[aggregateID]++
		seq := pos[aggregateID]
		e := domain.EventEnvelope[any]{
			ID: fmt.Sprintf("evt-%s-%d", aggregateID, seq), AggregateID: aggregateID, EventType: eventType,
			Payload: payload, Created: time.Now().UTC(), SequenceNo: seq, TransactionID: txID,
		}
		if err := env.rt.EventStore.Append(ctx, aggregateID, seq-1, e); err != nil {
			t.Fatalf("append %s for %s: %v", eventType, aggregateID, err)
		}
	}
	ldCtx := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	appendEvt("urn:type:course", "ResourceType.Created", entities.ResourceTypeCreated{
		Name: "Course", Slug: "course", Context: ldCtx, Timestamp: time.Now().UTC(),
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
	}, "")
	appendEvt("urn:type:course-instance", "ResourceType.Created", entities.ResourceTypeCreated{
		Name: "Course Instance", Slug: "course-instance", Context: ldCtx, Timestamp: time.Now().UTC(),
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},` +
			`"courseId":{"type":"string","x-resource-type":"course"}}}`),
	}, "")
	create := func(id, slug, agent, data string) {
		t.Helper()
		tx := "tx-" + id
		appendEvt(id, "Resource.Created", map[string]any{
			"TypeSlug": slug, "Data": json.RawMessage(data), "CreatedBy": agent, "AccountID": orphanAcct,
			"Timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		}, tx)
		appendEvt(id, "Resource.Published", entities.ResourcePublished{}.With(slug, orphanAcct), tx)
	}
	create("urn:course:1", "course", "agent-teacher", `{"@id":"urn:course:1","@type":"Course","name":"Coding"}`)
	create("urn:course-instance:1", "course-instance", "agent-planner",
		`{"@id":"urn:course-instance:1","@type":"CourseInstance","name":"Easter Camp","courseId":"urn:course:1"}`)
	for _, id := range []string{"urn:course:1", "urn:course-instance:1"} {
		appendEvt(id, "Resource.AccountAdopted", entities.ResourceAccountAdopted{
			FromAccountID: orphanAcct, AccountID: targetAcct, Timestamp: time.Now().UTC(),
		}, "tx-adopt-"+id)
	}
	// The planner reads the teacher's course through a grant.
	if err := env.db.Create(&models.ResourcePermission{
		ID: "perm-1", ResourceID: "urn:course:1", AgentID: "agent-planner",
		Actions: `["read"]`, GrantedBy: "agent-teacher", GrantedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	label := func(pass string) {
		t.Helper()
		var row struct {
			AccountID       string
			CourseIDDisplay *string
		}
		if err := env.db.Table("course_instances").Select("account_id, course_id_display").
			Where("id = ?", "urn:course-instance:1").Take(&row).Error; err != nil {
			t.Fatalf("%s: read the instance row: %v", pass, err)
		}
		if row.AccountID != targetAcct {
			t.Errorf("%s: instance row owner = %q, want %s", pass, row.AccountID, targetAcct)
		}
		if row.CourseIDDisplay == nil || *row.CourseIDDisplay != "Coding" {
			t.Errorf("%s: instance course label = %v, want Coding", pass, row.CourseIDDisplay)
		}
	}
	if _, err := Reproject(ctx, env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("first reproject: %v", err)
	}
	label("after the first reproject")
	if _, err := Reproject(ctx, env.rt, ReprojectOptions{}); err != nil {
		t.Fatalf("second reproject: %v", err)
	}
	label("after a second reproject over the moved rows")
}
