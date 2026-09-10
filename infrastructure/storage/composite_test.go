package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage"

	"github.com/segmentio/ksuid"
)

type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

type capturingFileService struct {
	url        string
	gotID      string
	gotAccount string
	gotFname   string
	gotCType   string
	gotBody    []byte
	err        error

	deletedAccount string
	deleteErr      error
}

func (m *capturingFileService) DeleteAccountFolder(_ context.Context, accountID string) error {
	m.deletedAccount = accountID
	return m.deleteErr
}

func TestComposite_PassesAccountIDToEveryBackend(t *testing.T) {
	primary := &capturingFileService{}
	first := &capturingFileService{}
	second := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{first, second}, nopLogger{})
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg", AccountID: "acct_1"}
	if _, err := svc.Upload(context.Background(), params, strings.NewReader("body")); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	for name, backend := range map[string]*capturingFileService{"primary": primary, "first secondary": first, "second secondary": second} {
		if backend.gotAccount != "acct_1" {
			t.Errorf("%s received account %q, want %q", name, backend.gotAccount, "acct_1")
		}
	}
}

func (m *capturingFileService) Upload(
	_ context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	m.gotID = params.ID
	m.gotAccount = params.AccountID
	m.gotFname = params.Filename
	m.gotCType = params.ContentType
	if reader != nil {
		m.gotBody, _ = io.ReadAll(reader)
	}
	if m.err != nil {
		return nil, m.err
	}
	id := params.ID
	if id == "" {
		id = ksuid.New().String()
	}
	url := m.url
	if url == "" {
		url = "https://example/" + id
	}
	return &services.UploadResult{ID: id, URL: url}, nil
}

func TestComposite_PrefersSecondaryURL(t *testing.T) {
	primary := &capturingFileService{url: "https://cloud/obj"}
	secondary := &capturingFileService{url: "/api/uploads/files/local"}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	// Composite should prefer the secondary (local) URL since it's app-hosted.
	if result.URL != "/api/uploads/files/local" {
		t.Errorf("URL = %q, want %q", result.URL, "/api/uploads/files/local")
	}
}

func TestComposite_SharedIDAcrossBackends(t *testing.T) {
	primary := &capturingFileService{}
	secondary := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	// Both backends must receive the same pre-generated ID.
	if primary.gotID == "" {
		t.Fatal("primary received empty ID")
	}
	if primary.gotID != secondary.gotID {
		t.Errorf("IDs differ: primary=%q secondary=%q", primary.gotID, secondary.gotID)
	}
}

func TestComposite_SecondariesReceiveCorrectData(t *testing.T) {
	primary := &capturingFileService{}
	secondary := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{secondary}, nopLogger{})
	body := "file-content-here"
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader(body))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}

	if primary.gotFname != "photo.jpg" {
		t.Errorf("primary filename = %q, want %q", primary.gotFname, "photo.jpg")
	}
	if string(primary.gotBody) != body {
		t.Errorf("primary body = %q, want %q", primary.gotBody, body)
	}
	if secondary.gotFname != "photo.jpg" {
		t.Errorf("secondary filename = %q, want %q", secondary.gotFname, "photo.jpg")
	}
	if string(secondary.gotBody) != body {
		t.Errorf("secondary body = %q, want %q", secondary.gotBody, body)
	}
}

func TestComposite_PrimaryFailure(t *testing.T) {
	primary := &capturingFileService{err: errors.New("primary failure")}

	svc := storage.NewComposite(primary, nil, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from primary failure")
	}
}

func TestComposite_SecondaryFailureNonFatalWhenAnotherSucceeds(t *testing.T) {
	primary := &capturingFileService{url: "https://storage.googleapis.com/bucket/obj"}
	failing := &capturingFileService{err: errors.New("secondary down")}
	working := &capturingFileService{url: "/api/uploads/files/accounts/acct_1/uploads/obj"}

	svc := storage.NewComposite(primary, []services.FileService{failing, working}, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain", AccountID: "acct_1"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() should not fail while one secondary succeeds: %v", err)
	}
	if result.URL != working.url {
		t.Errorf("URL = %q, want the working secondary's %q", result.URL, working.url)
	}
}

// A bucket URL is never checked against the caller's account, so the composite
// must not hand one out when no app-hosted replica exists.
func TestComposite_EverySecondaryFailingFailsTheUpload(t *testing.T) {
	const bucketURL = "https://storage.googleapis.com/bucket/accounts/acct_1/uploads/obj"
	primary := &capturingFileService{url: bucketURL}
	first := &capturingFileService{err: errors.New("disk full")}
	second := &capturingFileService{err: errors.New("permission denied")}

	svc := storage.NewComposite(primary, []services.FileService{first, second}, nopLogger{})
	params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg", AccountID: "acct_1"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err == nil {
		t.Fatalf("Upload() succeeded with result %+v, want an error when every secondary fails", result)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
	if strings.Contains(err.Error(), bucketURL) {
		t.Errorf("error %q carries the bucket URL", err.Error())
	}
}

func TestComposite_NoSecondariesReturnsPrimaryResult(t *testing.T) {
	primary := &capturingFileService{url: "https://cloud/ok"}

	svc := storage.NewComposite(primary, nil, nopLogger{})
	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain", AccountID: "acct_1"}
	result, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if result.URL != "https://cloud/ok" {
		t.Errorf("URL = %q, want %q", result.URL, "https://cloud/ok")
	}
}

type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestComposite_BufferingFailure(t *testing.T) {
	primary := &capturingFileService{}
	svc := storage.NewComposite(primary, nil, nopLogger{})

	params := services.UploadParams{Filename: "test.txt", ContentType: "text/plain"}
	_, err := svc.Upload(context.Background(), params, errReader{errors.New("stream broken")})
	if err == nil {
		t.Fatal("expected error from broken reader")
	}
	if !strings.Contains(err.Error(), "spool upload data") {
		t.Errorf("error = %q, want to contain 'spool upload data'", err.Error())
	}
}

func TestComposite_DeleteAccountFolderAsksEveryBackend(t *testing.T) {
	primary := &capturingFileService{}
	first := &capturingFileService{}
	second := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{first, second}, nopLogger{})
	if err := svc.DeleteAccountFolder(context.Background(), "acct_1"); err != nil {
		t.Fatalf("DeleteAccountFolder() error: %v", err)
	}
	for name, backend := range map[string]*capturingFileService{"primary": primary, "first secondary": first, "second secondary": second} {
		if backend.deletedAccount != "acct_1" {
			t.Errorf("%s was asked to delete %q, want %q", name, backend.deletedAccount, "acct_1")
		}
	}
}

// A secondary that keeps the folder is the account's data still on the
// instance, so its failure is reported — alongside every other failure, not
// instead of the primary's, and without sparing the backends after it.
func TestComposite_DeleteAccountFolderJoinsEveryFailure(t *testing.T) {
	primary := &capturingFileService{deleteErr: errors.New("bucket refused")}
	first := &capturingFileService{deleteErr: errors.New("disk read-only")}
	second := &capturingFileService{}

	svc := storage.NewComposite(primary, []services.FileService{first, second}, nopLogger{})
	err := svc.DeleteAccountFolder(context.Background(), "acct_1")
	if err == nil {
		t.Fatal("DeleteAccountFolder() returned nil, want the joined failures")
	}
	for _, want := range []string{"bucket refused", "disk read-only"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not report %q", err, want)
		}
	}
	if second.deletedAccount != "acct_1" {
		t.Errorf("the backend after a failure was not asked: got %q", second.deletedAccount)
	}
}
