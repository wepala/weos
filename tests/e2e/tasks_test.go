package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	env := setupTestEnv(t)

	resp := env.doRequest(t, "GET", "/api/health", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPresetInstallation(t *testing.T) {
	env := setupTestEnv(t)

	// Verify the tasks preset resource types exist
	resp := env.doRequest(t, "GET", "/api/resource-types", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list resource types: expected 200, got %d", resp.StatusCode)
	}
	result := readJSON(t, resp)
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("expected data array in response: %v", result)
	}

	slugs := make(map[string]bool)
	for _, item := range data {
		m, _ := item.(map[string]any)
		if slug, ok := m["slug"].(string); ok {
			slugs[slug] = true
		}
	}

	if !slugs["project"] {
		t.Error("resource type 'project' not found after preset install")
	}
	if !slugs["task"] {
		t.Error("resource type 'task' not found after preset install")
	}
}

func TestCreateProject_Anonymous(t *testing.T) {
	env := setupTestEnv(t)

	body := `{"name":"Anonymous Project","description":"no auth","status":"active"}`
	resp := env.doRequest(t, "POST", "/api/project", body, "")
	if resp.StatusCode != http.StatusCreated {
		result := readJSON(t, resp)
		t.Fatalf("expected 201, got %d: %v", resp.StatusCode, result)
	}
	data := readEnvelopeData(t, resp)
	if data["id"] == nil || data["id"] == "" {
		t.Fatal("expected non-empty id")
	}
	if data["type_slug"] != "project" {
		t.Fatalf("type_slug = %v, want project", data["type_slug"])
	}
}

func TestCreateProject_Authenticated(t *testing.T) {
	env := setupTestEnv(t)

	body := `{"name":"Admin Project","description":"owned by admin","status":"active"}`
	resp := env.doRequest(t, "POST", "/api/project", body, "admin@weos.dev")
	if resp.StatusCode != http.StatusCreated {
		result := readJSON(t, resp)
		t.Fatalf("expected 201, got %d: %v", resp.StatusCode, result)
	}
	data := readEnvelopeData(t, resp)
	id := data["id"].(string)

	// Verify admin can read it back
	getResp := env.doRequest(t, "GET", "/api/project/"+id, "", "admin@weos.dev")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get project: expected 200, got %d", getResp.StatusCode)
	}
	getData := readEnvelopeData(t, getResp)
	if getData["id"] != id {
		t.Fatalf("got id %v, want %v", getData["id"], id)
	}
}

func TestCreateTask_WithProjectReference(t *testing.T) {
	env := setupTestEnv(t)

	// Create a project first
	projectID := env.seedProjectForUser(t, "Ref Test Project", "admin@weos.dev")

	// Create a task linked to the project
	body := fmt.Sprintf(`{
		"name": "Linked Task",
		"status": "open",
		"priority": "high",
		"dueDate": "2026-05-01",
		"project": %q
	}`, projectID)
	resp := env.doRequest(t, "POST", "/api/task", body, "admin@weos.dev")
	if resp.StatusCode != http.StatusCreated {
		result := readJSON(t, resp)
		t.Fatalf("expected 201, got %d: %v", resp.StatusCode, result)
	}
	data := readEnvelopeData(t, resp)
	taskID := data["id"].(string)

	// Verify the task exists
	getResp := env.doRequest(t, "GET", "/api/task/"+taskID, "", "admin@weos.dev")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d", getResp.StatusCode)
	}
	getResp.Body.Close()
}

// TestCreateResource_EdgesReadableAfterCreate verifies the exact regression
// that issue #8 was filed against: after Create, the entity's @graph body
// must contain the reference as an edge so EdgeValue (and the projection's
// extractEdgeColumns) can find it. If BuildResourceGraph is ever called
// with stripped data + nil refProps again, this test will catch it.
func TestCreateResource_EdgesReadableAfterCreate(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Edge Test Project", "admin@weos.dev")
	taskBody := fmt.Sprintf(`{
		"name": "Edge-Carrying Task",
		"status": "open",
		"project": %q
	}`, projectID)
	resp := env.doRequest(t, "POST", "/api/task", taskBody, "admin@weos.dev")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d", resp.StatusCode)
	}
	taskID := readEnvelopeData(t, resp)["id"].(string)

	// The fix-sensitive path: filtering tasks by the project reference exercises
	// the projection's FK column, which is populated from the edges node of the
	// entity's @graph. Before the fix, this column stayed NULL because
	// BuildResourceGraph produced a @graph with no edges node.
	listURL := fmt.Sprintf("/api/task?_filter[project][eq]=%s", projectID)
	listResp := env.doRequest(t, "GET", listURL, "", "admin@weos.dev")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list by project: expected 200, got %d", listResp.StatusCode)
	}
	result := readJSON(t, listResp)
	rows, _ := result["data"].([]any)
	found := false
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if id, _ := m["id"].(string); id == taskID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("task %s not returned by project filter — edges are not being stored on Create", taskID)
	}
}

// TestUpdateResource_EdgesReconciled verifies the Update path stores edges
// symmetrically to Create. Changing the project reference must be observable
// through the FK-filtered list: the old reference no longer matches, the
// new one does.
func TestUpdateResource_EdgesReconciled(t *testing.T) {
	env := setupTestEnv(t)

	project1 := env.seedProjectForUser(t, "Update Test Project 1", "admin@weos.dev")
	project2 := env.seedProjectForUser(t, "Update Test Project 2", "admin@weos.dev")

	// Create a task in project1.
	body := fmt.Sprintf(`{"name":"Movable Task","status":"open","project":%q}`, project1)
	resp := env.doRequest(t, "POST", "/api/task", body, "admin@weos.dev")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d", resp.StatusCode)
	}
	taskID := readEnvelopeData(t, resp)["id"].(string)

	// Re-point it to project2 via Update.
	updateBody := fmt.Sprintf(`{"name":"Movable Task","status":"open","project":%q}`, project2)
	upResp := env.doRequest(t, "PUT", "/api/task/"+taskID, updateBody, "admin@weos.dev")
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("update task: expected 200, got %d", upResp.StatusCode)
	}
	upResp.Body.Close()

	// Task should now be listed under project2, not project1.
	taskUnderProject := func(pid string) bool {
		url := fmt.Sprintf("/api/task?_filter[project][eq]=%s", pid)
		r := env.doRequest(t, "GET", url, "", "admin@weos.dev")
		defer r.Body.Close()
		result := readJSON(t, r)
		rows, _ := result["data"].([]any)
		for _, row := range rows {
			m, _ := row.(map[string]any)
			if id, _ := m["id"].(string); id == taskID {
				return true
			}
		}
		return false
	}
	if taskUnderProject(project1) {
		t.Errorf("task still returned under original project after Update — stale edge not reconciled")
	}
	if !taskUnderProject(project2) {
		t.Errorf("task not returned under new project after Update — new edge not stored")
	}
}

func TestListProjects_Authenticated(t *testing.T) {
	env := setupTestEnv(t)

	// Create two projects as admin
	env.seedProjectForUser(t, "List Project A", "admin@weos.dev")
	env.seedProjectForUser(t, "List Project B", "admin@weos.dev")

	resp := env.doRequest(t, "GET", "/api/project", "", "admin@weos.dev")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list projects: expected 200, got %d", resp.StatusCode)
	}
	result := readJSON(t, resp)
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("expected data array: %v", result)
	}
	if len(data) < 2 {
		t.Fatalf("expected at least 2 projects, got %d", len(data))
	}
}

func TestListTasks_ByProject(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Filter Project", "admin@weos.dev")
	env.seedTaskForUser(t, "Task In Project", projectID, "admin@weos.dev")

	// List tasks filtered by project
	url := fmt.Sprintf("/api/task?_filter[project][eq]=%s", projectID)
	resp := env.doRequest(t, "GET", url, "", "admin@weos.dev")
	if resp.StatusCode != http.StatusOK {
		result := readJSON(t, resp)
		t.Fatalf("list filtered tasks: expected 200, got %d: %v", resp.StatusCode, result)
	}
	result := readJSON(t, resp)
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("expected data array: %v", result)
	}
	if len(data) == 0 {
		t.Fatal("expected at least 1 task for the project filter")
	}
}

func TestUpdateProject(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Update Me", "admin@weos.dev")

	body := `{"name":"Updated Name","description":"updated","status":"archived"}`
	resp := env.doRequest(t, "PUT", "/api/project/"+projectID, body, "admin@weos.dev")
	if resp.StatusCode != http.StatusOK {
		result := readJSON(t, resp)
		t.Fatalf("update project: expected 200, got %d: %v", resp.StatusCode, result)
	}
	resp.Body.Close()
}

func TestDeleteTask(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Delete Project", "admin@weos.dev")
	taskID := env.seedTaskForUser(t, "Delete Me", projectID, "admin@weos.dev")

	resp := env.doRequest(t, "DELETE", "/api/task/"+taskID, "", "admin@weos.dev")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete task: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOwnership_CreatorCanAccess(t *testing.T) {
	env := setupTestEnv(t)

	// Admin creates a project
	projectID := env.seedProjectForUser(t, "Admin Only", "admin@weos.dev")

	// Admin can access it
	resp := env.doRequest(t, "GET", "/api/project/"+projectID, "", "admin@weos.dev")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin access own project: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOwnership_OtherUserDenied(t *testing.T) {
	env := setupTestEnv(t)

	// Admin creates a project
	projectID := env.seedProjectForUser(t, "Admin Private", "admin@weos.dev")

	// Member tries to access it — should be denied with ErrorEnvelope
	resp := env.doRequest(t, "GET", "/api/project/"+projectID, "", "member@weos.dev")
	if resp.StatusCode != http.StatusForbidden {
		result := readJSON(t, resp)
		t.Fatalf("member access admin project: expected 403, got %d: %v", resp.StatusCode, result)
	}
	result := readJSON(t, resp)
	if result["error"] == nil || result["error"] == "" {
		t.Fatalf("expected 'error' key in 403 response, got: %v", result)
	}
}

func TestOwnership_ListOnlyShowsOwnResources(t *testing.T) {
	env := setupTestEnv(t)

	// Admin creates a project
	env.seedProjectForUser(t, "Admin List Project", "admin@weos.dev")

	// Member creates a project
	env.seedProjectForUser(t, "Member List Project", "member@weos.dev")

	// Member lists projects — should only see their own
	resp := env.doRequest(t, "GET", "/api/project", "", "member@weos.dev")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list projects: expected 200, got %d", resp.StatusCode)
	}
	result := readJSON(t, resp)
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("expected data array: %v", result)
	}

	for _, item := range data {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		if name == "Admin List Project" {
			t.Fatal("member should not see admin's project in list")
		}
	}
}

func TestOwnership_MemberCanUpdateOwnResource(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Member Project", "member@weos.dev")

	body := `{"name":"Member Updated","description":"updated by member","status":"active"}`
	resp := env.doRequest(t, "PUT", "/api/project/"+projectID, body, "member@weos.dev")
	if resp.StatusCode != http.StatusOK {
		result := readJSON(t, resp)
		t.Fatalf("member update own: expected 200, got %d: %v", resp.StatusCode, result)
	}
	resp.Body.Close()
}

func TestOwnership_MemberCannotUpdateOtherResource(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Admin Exclusive", "admin@weos.dev")

	body := `{"name":"Hacked","description":"should fail","status":"active"}`
	resp := env.doRequest(t, "PUT", "/api/project/"+projectID, body, "member@weos.dev")
	if resp.StatusCode != http.StatusForbidden {
		result := readJSON(t, resp)
		t.Fatalf("member update admin project: expected 403, got %d: %v", resp.StatusCode, result)
	}
	resp.Body.Close()
}

func TestOwnership_MemberCannotDeleteOtherResource(t *testing.T) {
	env := setupTestEnv(t)

	projectID := env.seedProjectForUser(t, "Admin No Delete", "admin@weos.dev")

	resp := env.doRequest(t, "DELETE", "/api/project/"+projectID, "", "member@weos.dev")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member delete admin project: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestFullWorkflow_ProjectsAndTasks(t *testing.T) {
	env := setupTestEnv(t)

	// 1. Create a project
	projectBody := `{"name":"Workflow Project","description":"E2E test","status":"active"}`
	createProjResp := env.doRequest(t, "POST", "/api/project", projectBody, "admin@weos.dev")
	if createProjResp.StatusCode != http.StatusCreated {
		result := readJSON(t, createProjResp)
		t.Fatalf("step 1 create project: expected 201, got %d: %v", createProjResp.StatusCode, result)
	}
	projData := readEnvelopeData(t, createProjResp)
	projectID := projData["id"].(string)

	// 2. Create tasks linked to the project
	task1Body := fmt.Sprintf(`{"name":"Task Alpha","status":"open","priority":"high","dueDate":"2026-06-01","project":%q}`, projectID)
	createTask1Resp := env.doRequest(t, "POST", "/api/task", task1Body, "admin@weos.dev")
	if createTask1Resp.StatusCode != http.StatusCreated {
		result := readJSON(t, createTask1Resp)
		t.Fatalf("step 2a create task: expected 201, got %d: %v", createTask1Resp.StatusCode, result)
	}
	task1Data := readEnvelopeData(t, createTask1Resp)
	task1ID := task1Data["id"].(string)

	task2Body := fmt.Sprintf(`{"name":"Task Beta","status":"in-progress","priority":"low","project":%q}`, projectID)
	createTask2Resp := env.doRequest(t, "POST", "/api/task", task2Body, "admin@weos.dev")
	if createTask2Resp.StatusCode != http.StatusCreated {
		result := readJSON(t, createTask2Resp)
		t.Fatalf("step 2b create task: expected 201, got %d: %v", createTask2Resp.StatusCode, result)
	}
	createTask2Resp.Body.Close()

	// 3. List tasks — should have at least 2
	listResp := env.doRequest(t, "GET", "/api/task", "", "admin@weos.dev")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("step 3 list tasks: expected 200, got %d", listResp.StatusCode)
	}
	listResult := readJSON(t, listResp)
	tasks, _ := listResult["data"].([]any)
	if len(tasks) < 2 {
		t.Fatalf("step 3: expected >= 2 tasks, got %d", len(tasks))
	}

	// 4. Update task status
	updateBody := fmt.Sprintf(`{"name":"Task Alpha","status":"done","priority":"high","dueDate":"2026-06-01","project":%q}`, projectID)
	updateResp := env.doRequest(t, "PUT", "/api/task/"+task1ID, updateBody, "admin@weos.dev")
	if updateResp.StatusCode != http.StatusOK {
		result := readJSON(t, updateResp)
		t.Fatalf("step 4 update task: expected 200, got %d: %v", updateResp.StatusCode, result)
	}
	updateResp.Body.Close()

	// 5. Get updated task — verify it's still accessible
	getResp := env.doRequest(t, "GET", "/api/task/"+task1ID, "", "admin@weos.dev")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("step 5 get task: expected 200, got %d", getResp.StatusCode)
	}
	getResp.Body.Close()

	// 6. Delete a task
	delResp := env.doRequest(t, "DELETE", "/api/task/"+task1ID, "", "admin@weos.dev")
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("step 6 delete task: expected 204, got %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// 7. Verify project still exists
	projGetResp := env.doRequest(t, "GET", "/api/project/"+projectID, "", "admin@weos.dev")
	if projGetResp.StatusCode != http.StatusOK {
		t.Fatalf("step 7 get project: expected 200, got %d", projGetResp.StatusCode)
	}
	projGetResp.Body.Close()
}
