package cloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"pocketdrive/internal/db"
)

func TestS3RenameProtectsDestination(t *testing.T) {
	for _, mode := range []string{"existing", "directory", "race", "denied", "success"} {
		t.Run(mode, func(t *testing.T) {
			puts, deletes := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("list-type") == "2" {
					w.Header().Set("Content-Type", "application/xml")
					if mode == "directory" {
						io.WriteString(w, `<ListBucketResult><Contents><Key>target/keep</Key><Size>3</Size></Contents></ListBucketResult>`)
					} else {
						io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
					}
					return
				}
				if r.Method == "HEAD" && r.URL.Path == "/bucket/target" {
					switch mode {
					case "existing":
						w.Header().Set("Content-Length", "3")
						w.Header().Set("Last-Modified", "Mon, 07 Sep 2026 00:00:00 GMT")
						w.WriteHeader(200)
					case "denied":
						w.WriteHeader(403)
					default:
						w.WriteHeader(404)
					}
					return
				}
				if r.URL.Path == "/bucket/source" && (r.Method == "HEAD" || r.Method == "GET") {
					w.Header().Set("Content-Length", "3")
					w.Header().Set("Last-Modified", "Mon, 07 Sep 2026 00:00:00 GMT")
					w.Header().Set("ETag", `"source-etag"`)
					if r.Method == "GET" {
						io.WriteString(w, "old")
					}
					return
				}
				if r.Method == "PUT" && r.URL.Path == "/bucket/target" {
					puts++
					if r.Header.Get("If-None-Match") != "*" {
						t.Error("destination PUT missing no-replace condition")
					}
					io.Copy(io.Discard, r.Body)
					if mode == "race" {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(412)
						io.WriteString(w, `<Error><Code>PreconditionFailed</Code><Message>Destination exists</Message></Error>`)
					} else {
						w.Header().Set("ETag", `"new-etag"`)
					}
					return
				}
				if r.Method == "DELETE" {
					deletes++
					w.WriteHeader(204)
					return
				}
				t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				w.WriteHeader(500)
			}))
			defer srv.Close()
			m, err := newS3Mount(&db.StoragePolicy{Name: "test", Endpoint: srv.URL, Bucket: "bucket", Region: "us-east-1", AccessKey: "test", SecretKey: "test"})
			if err != nil {
				t.Fatal(err)
			}
			err = m.Rename(context.Background(), "source", "target")
			if mode == "success" {
				if err != nil || puts != 1 || deletes != 1 {
					t.Fatalf("err=%v puts=%d deletes=%d", err, puts, deletes)
				}
			} else {
				if err == nil || deletes != 0 {
					t.Fatalf("source not protected: err=%v deletes=%d", err, deletes)
				}
				if mode != "denied" && !errors.Is(err, os.ErrExist) {
					t.Fatalf("want conflict, got %v", err)
				}
				if !strings.Contains(mode, "race") && puts != 0 {
					t.Fatalf("unexpected writes: %d", puts)
				}
			}
		})
	}
}
