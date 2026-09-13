package cmd

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestGmailCorrelationMessageIDIsStableAndOpaque(t *testing.T) {
	first, err := gmailCorrelationMessageID("sportnet-abc123")
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	second, err := gmailCorrelationMessageID("sportnet-abc123")
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	other, err := gmailCorrelationMessageID("sportnet-def456")
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	if first != second || first == other {
		t.Fatalf("message IDs are not deterministic and unique: %q %q %q", first, second, other)
	}
	if strings.Contains(first, "abc123") {
		t.Fatalf("RFC Message-ID leaked the opaque correlation value: %q", first)
	}
}

func TestGmailSendCorrelationSerializesAndVerifiesProviderCopy(t *testing.T) {
	const correlationID = "sportnet-abc123"
	const replyTo = "reply+opaque123@reply.sportnet.ai"
	expectedRFCMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}

	var sentHeaders mail.Header
	var sendCalls, metadataCalls int
	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages":
			writeJSONResponse(t, w, map[string]any{"messages": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send":
			sendCalls++
			var request gmail.Message
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode send request: %v", err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(request.Raw)
			if err != nil {
				t.Fatalf("decode raw message: %v", err)
			}
			parsed, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(raw)))
			if err != nil {
				t.Fatalf("parse raw message: %v", err)
			}
			sentHeaders = parsed.Header
			writeJSONResponse(t, w, map[string]any{"id": "provider-1", "threadId": "thread-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/provider-1":
			metadataCalls++
			writeJSONResponse(t, w, gmailProviderMetadata("provider-1", "thread-1", expectedRFCMessageID, correlationID, replyTo))
		default:
			http.Error(w, "not available", http.StatusNotFound)
		}
	})
	defer cleanup()

	output := runGmailSendJSON(t, &GmailSendCmd{
		To:            "coach@example.com",
		Subject:       "Hello",
		Body:          "Body",
		ReplyTo:       replyTo,
		CorrelationID: correlationID,
	}, svc, nil)

	if sendCalls != 1 || metadataCalls != 1 {
		t.Fatalf("unexpected provider calls: send=%d metadata=%d", sendCalls, metadataCalls)
	}
	if got := sentHeaders.Get("Reply-To"); got != replyTo {
		t.Fatalf("Reply-To = %q, want %q", got, replyTo)
	}
	if got := sentHeaders.Get(gmailCorrelationHeader); got != correlationID {
		t.Fatalf("correlation header = %q, want %q", got, correlationID)
	}
	if got := sentHeaders.Get("Message-ID"); got != expectedRFCMessageID {
		t.Fatalf("Message-ID = %q, want %q", got, expectedRFCMessageID)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	for key, want := range map[string]string{
		"messageId":     "provider-1",
		"threadId":      "thread-1",
		"rfcMessageId":  expectedRFCMessageID,
		"correlationId": correlationID,
		"replyTo":       replyTo,
	} {
		if got := result[key]; got != want {
			t.Fatalf("%s = %#v, want %q", key, got, want)
		}
	}
}

func TestGmailCorrelationProviderOperationsRequireExplicitAccount(t *testing.T) {
	t.Setenv("GOG_ACCOUNT", "wrong-default@example.com")
	ctx := newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard)
	if err := (&GmailMessagesLookupCorrelationCmd{CorrelationID: "sportnet-abc123"}).Run(ctx, &RootFlags{}); err == nil || !strings.Contains(err.Error(), "explicit --account") {
		t.Fatalf("lookup should reject default account fallback, got %v", err)
	}
	if err := (&GmailSendCmd{
		To: "coach@example.com", Subject: "S", Body: "B",
		ReplyTo: "reply+opaque@reply.sportnet.ai", CorrelationID: "sportnet-abc123",
	}).Run(ctx, &RootFlags{}); err == nil || !strings.Contains(err.Error(), "explicit --account") {
		t.Fatalf("send should reject default account fallback, got %v", err)
	}
}

func TestGmailCorrelationInputsFailClosed(t *testing.T) {
	ctx := newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard)
	for _, correlationID := range []string{" leading", "trailing ", "bad\nheader", strings.Repeat("a", gmailCorrelationMaxLen+1)} {
		err := (&GmailMessagesLookupCorrelationCmd{CorrelationID: correlationID}).Run(ctx, &RootFlags{Account: "a@b.com"})
		if err == nil {
			t.Fatalf("expected invalid correlation %q to fail", correlationID)
		}
	}
	for _, replyTo := range []string{"", "Name <reply@example.com>", "a@example.com, b@example.com", "bad\n@example.com"} {
		err := (&GmailSendCmd{
			To: "coach@example.com", Subject: "S", Body: "B",
			ReplyTo: replyTo, CorrelationID: "sportnet-abc123",
		}).Run(ctx, &RootFlags{Account: "a@b.com"})
		if err == nil {
			t.Fatalf("expected invalid Reply-To %q to fail", replyTo)
		}
	}
}

func TestGmailSendCorrelationRefusesVisibleDuplicateOrAmbiguousRetry(t *testing.T) {
	const correlationID = "sportnet-idempotent123"
	const replyTo = "reply+idempotent123@reply.sportnet.ai"
	rfcMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	for _, count := range []int{1, 2} {
		t.Run(string(rune('0'+count))+"-existing", func(t *testing.T) {
			var sendCalls int
			svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages":
					messages := make([]map[string]any, 0, count)
					for i := 0; i < count; i++ {
						messages = append(messages, map[string]any{"id": "existing-" + string(rune('1'+i))})
					}
					writeJSONResponse(t, w, map[string]any{"messages": messages})
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/existing-"):
					id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
					writeJSONResponse(t, w, gmailProviderMetadata(id, "", rfcMessageID, correlationID, replyTo))
				case r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send":
					sendCalls++
					writeJSONResponse(t, w, map[string]any{"id": "unexpected"})
				default:
					http.NotFound(w, r)
				}
			})
			defer cleanup()
			ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
			err := (&GmailSendCmd{
				To: "coach@example.com", Subject: "S", Body: "B",
				ReplyTo: replyTo, CorrelationID: correlationID,
			}).Run(ctx, &RootFlags{Account: "pinned@example.com"})
			if err == nil || (!strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "ambiguous")) {
				t.Fatalf("expected duplicate retry refusal, got %v", err)
			}
			if sendCalls != 0 {
				t.Fatalf("duplicate marker caused %d sends", sendCalls)
			}
		})
	}
}

func TestGmailLookupCorrelationDistinguishesZeroOneAndMultiple(t *testing.T) {
	const correlationID = "sportnet-abc123"
	rfcMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	for _, count := range []int{0, 1, 2} {
		t.Run(string(rune('0'+count))+"-matches", func(t *testing.T) {
			messages := make([]map[string]any, 0, count)
			for i := 0; i < count; i++ {
				messages = append(messages, map[string]any{"id": "provider-" + string(rune('1'+i)), "threadId": "thread-1"})
			}
			var observedQuery string
			svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages":
					observedQuery = r.URL.Query().Get("q")
					writeJSONResponse(t, w, map[string]any{"messages": messages})
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/provider-"):
					id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
					writeJSONResponse(t, w, gmailProviderMetadata(id, "thread-1", rfcMessageID, correlationID, "reply@example.com"))
				default:
					http.NotFound(w, r)
				}
			})
			defer cleanup()

			var out bytes.Buffer
			ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
			if err := (&GmailMessagesLookupCorrelationCmd{CorrelationID: correlationID}).Run(ctx, &RootFlags{Account: "pinned@example.com"}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			var result struct {
				MatchCount int                     `json:"matchCount"`
				Matches    []gmailCorrelationMatch `json:"matches"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if result.MatchCount != count || len(result.Matches) != count {
				t.Fatalf("count=%d matches=%d, want %d", result.MatchCount, len(result.Matches), count)
			}
			wantQuery := "in:sent rfc822msgid:" + strings.Trim(rfcMessageID, "<>")
			if observedQuery != wantQuery {
				t.Fatalf("query = %q, want exact %q", observedQuery, wantQuery)
			}
		})
	}
}

func TestGmailLookupCorrelationRejectsMalformedAndAmbiguousProviderResults(t *testing.T) {
	const correlationID = "sportnet-abc123"
	rfcMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	tests := []struct {
		name     string
		list     []map[string]any
		metadata map[string]any
	}{
		{name: "missing provider id", list: []map[string]any{{"threadId": "t1"}}},
		{name: "duplicate provider id", list: []map[string]any{{"id": "m1"}, {"id": "m1"}}, metadata: gmailProviderMetadata("m1", "", rfcMessageID, correlationID, "reply@example.com")},
		{name: "wrong correlation", list: []map[string]any{{"id": "m1"}}, metadata: gmailProviderMetadata("m1", "", rfcMessageID, "sportnet-other", "reply@example.com")},
		{name: "wrong RFC message id", list: []map[string]any{{"id": "m1"}}, metadata: gmailProviderMetadata("m1", "", "<wrong@example.com>", correlationID, "reply@example.com")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/gmail/v1/users/me/messages" {
					writeJSONResponse(t, w, map[string]any{"messages": tc.list})
					return
				}
				if strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/") && tc.metadata != nil {
					writeJSONResponse(t, w, tc.metadata)
					return
				}
				http.NotFound(w, r)
			})
			defer cleanup()
			ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
			err := (&GmailMessagesLookupCorrelationCmd{CorrelationID: correlationID}).Run(ctx, &RootFlags{Account: "pinned@example.com"})
			if err == nil {
				t.Fatal("expected malformed provider result to fail closed")
			}
		})
	}
}

func TestGmailUncertainSendReconcilesFromProviderAfterFreshCommandInstance(t *testing.T) {
	const correlationID = "sportnet-restart123"
	const replyTo = "reply+restart123@reply.sportnet.ai"
	rfcMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		t.Fatalf("gmailCorrelationMessageID: %v", err)
	}
	providerHasMessage := false
	metadataAvailable := false
	var sendCalls int
	svc, cleanup := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send":
			sendCalls++
			providerHasMessage = true
			writeJSONResponse(t, w, map[string]any{"id": "provider-restart", "threadId": "thread-restart"})
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages":
			messages := []map[string]any{}
			if providerHasMessage {
				messages = append(messages, map[string]any{"id": "provider-restart", "threadId": "thread-restart"})
			}
			writeJSONResponse(t, w, map[string]any{"messages": messages})
		case r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/provider-restart":
			if !metadataAvailable {
				http.Error(w, "injected metadata outage", http.StatusServiceUnavailable)
				return
			}
			writeJSONResponse(t, w, gmailProviderMetadata("provider-restart", "thread-restart", rfcMessageID, correlationID, replyTo))
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
	err = (&GmailSendCmd{
		To: "coach@example.com", Subject: "S", Body: "B",
		ReplyTo: replyTo, CorrelationID: correlationID,
	}).Run(ctx, &RootFlags{Account: "pinned@example.com"})
	if err == nil || !strings.Contains(err.Error(), "outcome uncertain") {
		t.Fatalf("expected fail-closed uncertain result, got %v", err)
	}
	if sendCalls != 1 {
		t.Fatalf("send calls = %d, want 1", sendCalls)
	}

	metadataAvailable = true
	var out bytes.Buffer
	// A fresh command instance has no in-memory send registry. It reconciles
	// solely from the stable provider marker, as it would after a restart.
	freshCtx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
	if err := (&GmailMessagesLookupCorrelationCmd{CorrelationID: correlationID}).Run(freshCtx, &RootFlags{Account: "pinned@example.com"}); err != nil {
		t.Fatalf("fresh lookup: %v", err)
	}
	var result struct {
		MatchCount int                     `json:"matchCount"`
		Matches    []gmailCorrelationMatch `json:"matches"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode lookup: %v", err)
	}
	if result.MatchCount != 1 || len(result.Matches) != 1 || result.Matches[0].MessageID != "provider-restart" {
		t.Fatalf("unexpected reconciliation result: %#v", result)
	}
	if sendCalls != 1 {
		t.Fatalf("lookup retried the send; send calls = %d", sendCalls)
	}
}

func gmailProviderMetadata(id, threadID, rfcMessageID, correlationID, replyTo string) map[string]any {
	return map[string]any{
		"id":       id,
		"threadId": threadID,
		"payload": map[string]any{"headers": []map[string]any{
			{"name": "Message-ID", "value": rfcMessageID},
			{"name": gmailCorrelationHeader, "value": correlationID},
			{"name": "Reply-To", "value": replyTo},
		}},
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
