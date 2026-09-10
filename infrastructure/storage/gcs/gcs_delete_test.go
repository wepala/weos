package gcs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/infrastructure/storage/gcs"
)

// fakeBucket answers the JSON API's object list and delete, paging the
// listing so the walk has to follow nextPageToken, and records every delete.
type fakeBucket struct {
	t        *testing.T
	pageSize int
	failOn   string // object name whose delete answers 500

	mu      sync.Mutex
	objects []string
	deleted []string
	lists   []string // the prefix of every list request, in order
}

func (f *fakeBucket) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/b/"+testBucket+"/o"):
		f.serveList(rw, r)
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/b/"+testBucket+"/o/"):
		name := strings.TrimPrefix(r.URL.Path, strings.SplitAfter(r.URL.Path, "/b/"+testBucket+"/o/")[0])
		name = strings.ReplaceAll(name, "%2F", "/")
		if name == f.failOn {
			http.Error(rw, "bucket refused", http.StatusInternalServerError)
			return
		}
		f.deleted = append(f.deleted, name)
		rw.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(rw, "unexpected", http.StatusBadRequest)
	}
}

func (f *fakeBucket) serveList(rw http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	f.lists = append(f.lists, prefix)
	var matching []string
	for _, name := range f.objects {
		if strings.HasPrefix(name, prefix) {
			matching = append(matching, name)
		}
	}
	start := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		start, _ = strconv.Atoi(token)
	}
	end := len(matching)
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
	}
	items := make([]map[string]any, 0, end-start)
	for _, name := range matching[start:end] {
		items = append(items, map[string]any{"bucket": testBucket, "name": name})
	}
	body := map[string]any{"kind": "storage#objects", "items": items}
	if end < len(matching) {
		body["nextPageToken"] = fmt.Sprint(end)
	}
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(body)
}

func TestDeleteAccountFolder_DeletesEveryObjectUnderThePrefixAcrossPages(t *testing.T) {
	fake := &fakeBucket{t: t, pageSize: 2, objects: []string{
		"accounts/acct_1/uploads/a-one.jpg",
		"accounts/acct_1/uploads/b-two.jpg",
		"accounts/acct_1/uploads/c-three.jpg",
		"accounts/acct_1/uploads/d-four.jpg",
		"accounts/acct_1/uploads/e-five.jpg",
		"accounts/acct_10/uploads/f-other.jpg",
		"accounts/acct_2/uploads/g-other.jpg",
		"uploads/h-legacy-flat.jpg",
	}}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})

	if err := svc.DeleteAccountFolder(context.Background(), "acct_1"); err != nil {
		t.Fatalf("DeleteAccountFolder() error: %v", err)
	}

	want := fake.objects[:5]
	if len(fake.deleted) != len(want) {
		t.Fatalf("deleted %v, want exactly %v", fake.deleted, want)
	}
	for i, name := range want {
		if fake.deleted[i] != name {
			t.Errorf("deleted[%d] = %q, want %q", i, fake.deleted[i], name)
		}
	}
	for _, prefix := range fake.lists {
		if prefix != "accounts/acct_1/" {
			t.Errorf("listed with prefix %q, want accounts/acct_1/ (the trailing slash keeps acct_10 out)", prefix)
		}
	}
	if len(fake.lists) < 3 {
		t.Errorf("the listing was fetched %d times, want every page (3)", len(fake.lists))
	}
}

func TestDeleteAccountFolder_EmptyPrefixIsNotAnError(t *testing.T) {
	fake := &fakeBucket{t: t}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})
	if err := svc.DeleteAccountFolder(context.Background(), "acct_1"); err != nil {
		t.Fatalf("DeleteAccountFolder() on an empty prefix: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Errorf("deleted %v from an empty prefix", fake.deleted)
	}
}

func TestDeleteAccountFolder_ADeleteThatFailsFailsTheCall(t *testing.T) {
	fake := &fakeBucket{t: t, failOn: "accounts/acct_1/uploads/b-two.jpg", objects: []string{
		"accounts/acct_1/uploads/a-one.jpg",
		"accounts/acct_1/uploads/b-two.jpg",
		"accounts/acct_1/uploads/c-three.jpg",
	}}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})
	err := svc.DeleteAccountFolder(context.Background(), "acct_1")
	if err == nil || !strings.Contains(err.Error(), "b-two.jpg") {
		t.Fatalf("DeleteAccountFolder() error = %v, want the failed object named", err)
	}
}

func TestDeleteAccountFolder_RefusesUnsafeAccount(t *testing.T) {
	fake := &fakeBucket{t: t}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})
	for _, accountID := range []string{"", "../acct_2", "acct 1"} {
		err := svc.DeleteAccountFolder(context.Background(), accountID)
		if err == nil || !strings.Contains(err.Error(), "invalid account ID") {
			t.Errorf("DeleteAccountFolder(%q) error = %v, want an invalid account ID error", accountID, err)
		}
	}
	if len(fake.lists) != 0 {
		t.Errorf("the bucket was listed for an unsafe id: %v", fake.lists)
	}
}
