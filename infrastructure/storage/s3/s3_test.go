package s3_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	weoss3 "github.com/wepala/weos/v3/infrastructure/storage/s3"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
)

const (
	testBucket = "test-bucket"
	testRegion = "us-east-1"
)

type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

// capturingClient builds an S3 client that records every PutObject input and
// answers it itself, so no request leaves the process.
func capturingClient(puts *[]*s3sdk.PutObjectInput) *s3sdk.Client {
	capture := middleware.InitializeMiddlewareFunc("capturePutObject",
		func(_ context.Context, in middleware.InitializeInput, _ middleware.InitializeHandler) (
			middleware.InitializeOutput, middleware.Metadata, error,
		) {
			if put, ok := in.Parameters.(*s3sdk.PutObjectInput); ok {
				*puts = append(*puts, put)
			}
			return middleware.InitializeOutput{Result: &s3sdk.PutObjectOutput{}}, middleware.Metadata{}, nil
		})
	return s3sdk.New(s3sdk.Options{
		Region:      testRegion,
		Credentials: aws.AnonymousCredentials{},
		APIOptions: []func(*middleware.Stack) error{
			func(stack *middleware.Stack) error { return stack.Initialize.Add(capture, middleware.Before) },
		},
	})
}

func TestUpload_PutsUnderAccountFolder(t *testing.T) {
	var puts []*s3sdk.PutObjectInput
	svc := weoss3.New(capturingClient(&puts), testBucket, testRegion, nopLogger{})

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
	if len(puts) != 1 {
		t.Fatalf("PutObject called %d times, want 1", len(puts))
	}
	if got := aws.ToString(puts[0].Key); got != wantKey {
		t.Errorf("PutObjectInput.Key = %q, want %q", got, wantKey)
	}
	if got := aws.ToString(puts[0].Bucket); got != testBucket {
		t.Errorf("PutObjectInput.Bucket = %q, want %q", got, testBucket)
	}
	if want := "https://s3." + testRegion + ".amazonaws.com/" + testBucket + "/" + wantKey; result.URL != want {
		t.Errorf("URL = %q, want %q", result.URL, want)
	}
}

func TestUpload_RefusesUploadWithoutSafeAccount(t *testing.T) {
	for _, accountID := range []string{"", "../acct_2", "acct_1/../acct_2", `acct\1`, "acct 1"} {
		t.Run(fmt.Sprintf("%q", accountID), func(t *testing.T) {
			var puts []*s3sdk.PutObjectInput
			svc := weoss3.New(capturingClient(&puts), testBucket, testRegion, nopLogger{})

			params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg", AccountID: accountID}
			_, err := svc.Upload(context.Background(), params, strings.NewReader("data"))
			if err == nil || !strings.Contains(err.Error(), "invalid account ID") {
				t.Fatalf("Upload() error = %v, want an invalid account ID error", err)
			}
			if len(puts) != 0 {
				t.Errorf("PutObject called %d times, want none", len(puts))
			}
		})
	}
}
