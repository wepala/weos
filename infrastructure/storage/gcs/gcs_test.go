package gcs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage/gcs"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

const testBucket = "test-bucket"

type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

// fakeGCS answers the JSON API's object insert and records the name and bytes
// of every object it was sent.
type fakeGCS struct {
	t        *testing.T
	mu       sync.Mutex
	requests int
	names    []string
	bodies   []string
}

func (f *fakeGCS) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()

	name, body, err := objectFromInsert(r)
	if err != nil {
		f.t.Errorf("unexpected request %s %s: %v", r.Method, r.URL, err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.names = append(f.names, name)
	f.bodies = append(f.bodies, string(body))
	f.mu.Unlock()

	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]string{
		"bucket": testBucket, "name": name, "size": fmt.Sprint(len(body)),
	})
}

func objectFromInsert(r *http.Request) (string, []byte, error) {
	if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/b/"+testBucket+"/o") {
		return "", nil, fmt.Errorf("not an object insert into %s", testBucket)
	}
	switch uploadType := r.URL.Query().Get("uploadType"); uploadType {
	case "multipart":
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			return "", nil, fmt.Errorf("multipart insert with content type %q", r.Header.Get("Content-Type"))
		}
		parts := multipart.NewReader(r.Body, params["boundary"])
		meta, err := parts.NextPart()
		if err != nil {
			return "", nil, fmt.Errorf("read metadata part: %w", err)
		}
		var object struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(meta).Decode(&object); err != nil {
			return "", nil, fmt.Errorf("decode metadata part: %w", err)
		}
		media, err := parts.NextPart()
		if err != nil {
			return "", nil, fmt.Errorf("read media part: %w", err)
		}
		body, err := io.ReadAll(media)
		return object.Name, body, err
	case "media":
		body, err := io.ReadAll(r.Body)
		return r.URL.Query().Get("name"), body, err
	default:
		return "", nil, fmt.Errorf("upload type %q is not handled by this fake", uploadType)
	}
}

func newClient(t *testing.T, handler http.Handler) *gcsstorage.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := gcsstorage.NewClient(context.Background(),
		option.WithEndpoint(srv.URL+"/storage/v1/"),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestUpload_WritesUnderAccountFolder(t *testing.T) {
	fake := &fakeGCS{t: t}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})

	params := services.UploadParams{
		Filename:    "My Photo.jpg",
		ContentType: "image/jpeg",
		ID:          "fixed-id-123",
		AccountID:   "acct_1",
	}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("jpeg-bytes"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	const wantKey = "accounts/acct_1/uploads/fixed-id-123-My_Photo.jpg"
	if len(fake.names) != 1 || fake.names[0] != wantKey {
		t.Fatalf("objects written = %v, want exactly [%s]", fake.names, wantKey)
	}
	if fake.bodies[0] != "jpeg-bytes" {
		t.Errorf("object body = %q, want %q", fake.bodies[0], "jpeg-bytes")
	}
	if want := "https://storage.googleapis.com/" + testBucket + "/" + wantKey; result.URL != want {
		t.Errorf("URL = %q, want %q", result.URL, want)
	}
}

func TestUpload_RefusesUploadWithoutSafeAccount(t *testing.T) {
	for _, accountID := range []string{"", "../acct_2", "acct_1/../acct_2", `acct\1`, "acct 1"} {
		t.Run(fmt.Sprintf("%q", accountID), func(t *testing.T) {
			fake := &fakeGCS{t: t}
			svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})

			params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg", AccountID: accountID}
			_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
			if err == nil || !strings.Contains(err.Error(), "invalid account ID") {
				t.Fatalf("Upload() error = %v, want an invalid account ID error", err)
			}
			if fake.requests != 0 {
				t.Errorf("GCS received %d requests, want none", fake.requests)
			}
		})
	}
}
