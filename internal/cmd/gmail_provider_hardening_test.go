package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	localapi "github.com/steipete/gogcli/internal/googleapi"

	"google.golang.org/api/gmail/v1"
)

func TestGmailProviderIdentityHardening(t *testing.T) {
	rfc, _ := gmailCorrelationMessageID("corr-1")
	valid := func() *gmail.Message {
		return &gmail.Message{Id: "id-1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
			{Name: "Message-ID", Value: rfc}, {Name: gmailCorrelationHeader, Value: "corr-1"}, {Name: "Reply-To", Value: "reply@example.com"},
		}}}
	}
	for _, name := range []string{"Message-ID", gmailCorrelationHeader, "Reply-To"} {
		for _, value := range []string{"conflict", ""} {
			msg := valid()
			msg.Payload.Headers = append(msg.Payload.Headers, &gmail.MessagePartHeader{Name: strings.ToLower(name), Value: value})
			if _, err := verifiedGmailCorrelationMatch(msg, "id-1", "corr-1", rfc); err == nil {
				t.Fatalf("accepted duplicate %s", name)
			}
		}
	}
	if _, err := verifiedGmailCorrelationMatch(valid(), "wrong-id", "corr-1", rfc); err == nil {
		t.Fatal("accepted wrong ID")
	}
	if _, err := verifiedGmailCorrelationMatch(valid(), "id-1", "corr-1", rfc); err != nil {
		t.Fatal(err)
	}
}

func TestGmailProviderCorrelationPaginationHardening(t *testing.T) {
	for _, mode := range []string{"complete", "cycle", "duplicate", "failure", "wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			rfc, _ := gmailCorrelationMessageID("corr-1")
			svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/gmail/v1/users/me/messages" {
					token := r.URL.Query().Get("pageToken")
					if token != "" && mode == "failure" {
						http.Error(w, "provider unavailable", 503)
						return
					}
					messages := []map[string]any{}
					next := ""
					if token == "" {
						for i := 0; i < 100; i++ {
							messages = append(messages, map[string]any{"id": fmt.Sprintf("id-%d", i)})
						}
						next = "page-2"
					} else {
						id := "id-100"
						if mode == "duplicate" {
							id = "id-0"
						}
						messages = append(messages, map[string]any{"id": id})
						if mode == "cycle" {
							next = "page-2"
						}
					}
					writeJSONResponse(t, w, map[string]any{"messages": messages, "nextPageToken": next})
					return
				}
				id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
				if mode == "wrong-id" {
					id = "wrong"
				}
				writeJSONResponse(t, w, gmailProviderMetadata(id, "thread-1", rfc, "corr-1", "reply@example.com"))
			})
			defer cleanup()
			matches, _, err := lookupSentGmailCorrelation(context.Background(), svc, "corr-1")
			if mode == "complete" {
				if err != nil || len(matches) != 101 {
					t.Fatalf("count=%d error=%v", len(matches), err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", mode)
			}
		})
	}
}

func TestGmailProviderTypedNotFound(t *testing.T) {
	for _, code := range []int{200, 400, 401, 403, 404, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				if code == 200 {
					writeJSONResponse(t, w, map[string]any{"id": "wrong"})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				fmt.Fprintf(w, `{"error":{"code":%d,"message":"provider error","errors":[{"reason":"notFound","domain":"global"}]}}`, code)
			})
			defer cleanup()
			var out bytes.Buffer
			ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
			err := (&GmailGetCmd{MessageID: "id-1", NotFoundResult: true}).Run(ctx, &RootFlags{Account: "pinned@example.com"})
			if code == 404 {
				if err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if json.Unmarshal(out.Bytes(), &got) != nil || got["status"] != "not_found" || got["account"] != "pinned@example.com" || got["messageId"] != "id-1" {
					t.Fatalf("bad evidence: %s", &out)
				}
			} else if err == nil {
				t.Fatalf("classified %d as success: %s", code, &out)
			}
		})
	}
}

func TestGmailProviderSearchRejectsMalformedAndDuplicateIDs(t *testing.T) {
	for _, messages := range [][]*gmail.Message{{nil}, {{Id: ""}}, {{Id: "id-1"}, {Id: "id-1"}}} {
		if _, err := fetchMessageDetails(context.Background(), nil, messages, nil, time.UTC, false, "text"); err == nil {
			t.Fatal("accepted malformed search")
		}
	}
}

type providerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGmailProviderCorrelatedSendNeverReplays(t *testing.T) {
	for _, code := range []int{429, 500, 0} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			posts := 0
			base := providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
					if code == 0 {
						return nil, errors.New("connection lost after acceptance")
					}
				}
				recorder := httptest.NewRecorder()
				if r.Method == "POST" {
					recorder.Header().Set("Content-Type", "application/json")
					recorder.WriteHeader(code)
					fmt.Fprintf(recorder, "provider failed")
				} else {
					recorder.Header().Set("Content-Type", "application/json")
					fmt.Fprint(recorder, "{}")
				}
				return recorder.Result(), nil
			})
			svc, err := gmail.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: localapi.NewRetryTransport(base)}))
			if err != nil {
				t.Fatal(err)
			}
			ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
			err = (&GmailSendCmd{To: "recipient@example.com", Subject: "test", Body: "test", ReplyTo: "reply@example.com", CorrelationID: "corr-1"}).Run(ctx, &RootFlags{Account: "pinned@example.com"})
			if err == nil || !strings.Contains(err.Error(), "outcome uncertain") || posts != 1 {
				t.Fatalf("posts=%d error=%v", posts, err)
			}
		})
	}
}

func TestGmailProviderRejectsUnboundAuthentication(t *testing.T) {
	for _, flags := range []*RootFlags{{Account: "pinned@example.com", authMode: localapi.AuthModeADC}, {Account: "pinned@example.com", AccessToken: "synthetic-test-token"}} {
		if _, err := requireExplicitGmailAccount(flags); err == nil {
			t.Fatal("accepted authentication that ignores account")
		}
	}
}

func TestGmailProviderRejectsMalformedNotFound(t *testing.T) {
	for _, err := range []*googleapi.Error{{Code: 404, Body: "<html>404</html>"}, {Code: 404}, {Code: 404, Errors: []googleapi.ErrorItem{{Reason: "forbidden"}}}} {
		if isExactGmailNotFound(err) {
			t.Fatal("accepted ambiguous 404")
		}
	}
}

func TestGmailProviderNotFoundEnvelope(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"error":{"code":404,"status":"NOT_FOUND"}}`, true},
		{`{"error":{"code":404,"errors":[{"reason":"notFound"}]}}`, true},
		{`{"error":{"code":404,"status":"PERMISSION_DENIED","errors":[{"reason":"notFound"}]}}`, false},
		{`{"error":{"code":404,"status":"NOT_FOUND","errors":[{"reason":"forbidden"}]}}`, false},
		{`{"error":{"code":404}}`, false},
		{`{"error":{"code":403,"status":"NOT_FOUND"}}`, false},
		{`{"error":null}`, false},
	} {
		if got := isExactGmailNotFound(&googleapi.Error{Code: 404, Body: tc.body}); got != tc.want {
			t.Fatalf("got %v for %s", got, tc.body)
		}
	}
}
