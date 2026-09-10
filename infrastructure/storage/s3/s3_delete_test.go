package s3_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	weoss3 "github.com/wepala/weos/v3/infrastructure/storage/s3"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
)

// fakeListing answers ListObjectsV2 from a fixed key set, one page per call
// of pageSize keys, and records every DeleteObjects input. A key named in
// refuse comes back in the per-key Errors of the delete that carries it,
// which is how S3 reports a partial failure — with a 200.
type fakeListing struct {
	keys     []string
	pageSize int
	refuse   string

	lists   []*s3sdk.ListObjectsV2Input
	deletes []*s3sdk.DeleteObjectsInput
}

func (f *fakeListing) client() *s3sdk.Client {
	answer := middleware.InitializeMiddlewareFunc("fakeS3",
		func(_ context.Context, in middleware.InitializeInput, _ middleware.InitializeHandler) (
			middleware.InitializeOutput, middleware.Metadata, error,
		) {
			switch params := in.Parameters.(type) {
			case *s3sdk.ListObjectsV2Input:
				f.lists = append(f.lists, params)
				return middleware.InitializeOutput{Result: f.page(params)}, middleware.Metadata{}, nil
			case *s3sdk.DeleteObjectsInput:
				f.deletes = append(f.deletes, params)
				out := &s3sdk.DeleteObjectsOutput{}
				for _, key := range params.Delete.Objects {
					if aws.ToString(key.Key) == f.refuse {
						out.Errors = append(out.Errors, types.Error{
							Key: key.Key, Code: aws.String("AccessDenied"), Message: aws.String("bucket refused"),
						})
					}
				}
				return middleware.InitializeOutput{Result: out}, middleware.Metadata{}, nil
			default:
				return middleware.InitializeOutput{}, middleware.Metadata{}, fmt.Errorf("unexpected call %T", in.Parameters)
			}
		})
	return s3sdk.New(s3sdk.Options{
		Region:      testRegion,
		Credentials: aws.AnonymousCredentials{},
		APIOptions: []func(*middleware.Stack) error{
			func(stack *middleware.Stack) error { return stack.Initialize.Add(answer, middleware.Before) },
		},
	})
}

func (f *fakeListing) page(params *s3sdk.ListObjectsV2Input) *s3sdk.ListObjectsV2Output {
	prefix := aws.ToString(params.Prefix)
	var matching []string
	for _, key := range f.keys {
		if strings.HasPrefix(key, prefix) {
			matching = append(matching, key)
		}
	}
	start := 0
	if token := aws.ToString(params.ContinuationToken); token != "" {
		_, _ = fmt.Sscan(token, &start)
	}
	end := len(matching)
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
	}
	out := &s3sdk.ListObjectsV2Output{}
	for _, key := range matching[start:end] {
		out.Contents = append(out.Contents, types.Object{Key: aws.String(key)})
	}
	if end < len(matching) {
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String(fmt.Sprint(end))
	}
	return out
}

func accountKeys(account string, n int) []string {
	keys := make([]string, 0, n)
	for i := range n {
		keys = append(keys, fmt.Sprintf("accounts/%s/uploads/%05d-photo.jpg", account, i))
	}
	return keys
}

func TestDeleteAccountFolder_PagesTheListingAndBatchesDeletesByAThousand(t *testing.T) {
	fake := &fakeListing{pageSize: 1000}
	fake.keys = append(fake.keys, accountKeys("acct_1", 2500)...)
	fake.keys = append(fake.keys, accountKeys("acct_10", 3)...)
	fake.keys = append(fake.keys, "uploads/legacy-flat.jpg")
	svc := weoss3.New(fake.client(), testBucket, testRegion, nopLogger{})

	if err := svc.DeleteAccountFolder(context.Background(), "acct_1"); err != nil {
		t.Fatalf("DeleteAccountFolder() error: %v", err)
	}

	if len(fake.lists) != 3 {
		t.Errorf("ListObjectsV2 was called %d times, want 3 (every page of the listing)", len(fake.lists))
	}
	for _, list := range fake.lists {
		if aws.ToString(list.Prefix) != "accounts/acct_1/" {
			t.Errorf("listed with prefix %q, want accounts/acct_1/ (the trailing slash keeps acct_10 out)", aws.ToString(list.Prefix))
		}
	}
	deleted := 0
	for _, del := range fake.deletes {
		if n := len(del.Delete.Objects); n > 1000 || n == 0 {
			t.Errorf("a DeleteObjects call carried %d keys, want between 1 and 1000", n)
		}
		for _, key := range del.Delete.Objects {
			if !strings.HasPrefix(aws.ToString(key.Key), "accounts/acct_1/") {
				t.Errorf("deleted %q, which is not under acct_1", aws.ToString(key.Key))
			}
		}
		deleted += len(del.Delete.Objects)
	}
	if deleted != 2500 {
		t.Errorf("deleted %d keys, want all 2500 of acct_1's", deleted)
	}
}

func TestDeleteAccountFolder_ARefusedKeyFailsTheCall(t *testing.T) {
	fake := &fakeListing{keys: accountKeys("acct_1", 3), refuse: "accounts/acct_1/uploads/00001-photo.jpg"}
	svc := weoss3.New(fake.client(), testBucket, testRegion, nopLogger{})
	err := svc.DeleteAccountFolder(context.Background(), "acct_1")
	if err == nil || !strings.Contains(err.Error(), "00001-photo.jpg") {
		t.Fatalf("DeleteAccountFolder() error = %v, want the refused key named", err)
	}
}

func TestDeleteAccountFolder_EmptyPrefixIsNotAnError(t *testing.T) {
	fake := &fakeListing{}
	svc := weoss3.New(fake.client(), testBucket, testRegion, nopLogger{})
	if err := svc.DeleteAccountFolder(context.Background(), "acct_1"); err != nil {
		t.Fatalf("DeleteAccountFolder() on an empty prefix: %v", err)
	}
	if len(fake.deletes) != 0 {
		t.Errorf("DeleteObjects was called %d times for an empty prefix", len(fake.deletes))
	}
}

func TestDeleteAccountFolder_RefusesUnsafeAccount(t *testing.T) {
	fake := &fakeListing{}
	svc := weoss3.New(fake.client(), testBucket, testRegion, nopLogger{})
	for _, accountID := range []string{"", "../acct_2", "acct 1"} {
		err := svc.DeleteAccountFolder(context.Background(), accountID)
		if err == nil || !strings.Contains(err.Error(), "invalid account ID") {
			t.Errorf("DeleteAccountFolder(%q) error = %v, want an invalid account ID error", accountID, err)
		}
	}
	if len(fake.lists) != 0 {
		t.Errorf("the bucket was listed for an unsafe id")
	}
}
