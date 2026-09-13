package cmd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"google.golang.org/api/gmail/v1"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

const (
	gmailCorrelationHeader = "X-Sportnet-Correlation-ID"
	gmailCorrelationDomain = "correlation.sportnet.ai"
	gmailCorrelationMaxLen = 200
)

var gmailCorrelationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,199}$`)

type GmailMessagesLookupCorrelationCmd struct {
	CorrelationID string `arg:"" name:"correlationId" help:"Exact opaque X-Sportnet-Correlation-ID"`
}

type gmailCorrelationMatch struct {
	MessageID     string `json:"messageId"`
	ThreadID      string `json:"threadId,omitempty"`
	RFCMessageID  string `json:"rfcMessageId"`
	CorrelationID string `json:"correlationId"`
	ReplyTo       string `json:"replyTo,omitempty"`
}

func (c *GmailMessagesLookupCorrelationCmd) Run(ctx context.Context, flags *RootFlags) error {
	correlationID, err := validateGmailCorrelationID(c.CorrelationID)
	if err != nil {
		return err
	}
	if correlationID == "" {
		return usage("missing correlationId")
	}
	account, err := requireExplicitGmailAccount(flags)
	if err != nil {
		return err
	}
	svc, err := gmailService(ctx, account)
	if err != nil {
		return err
	}

	matches, rfcMessageID, err := lookupSentGmailCorrelation(ctx, svc, correlationID)
	if err != nil {
		return err
	}
	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, stdoutWriter(ctx), map[string]any{
			"correlationId": correlationID,
			"rfcMessageId":  rfcMessageID,
			"matchCount":    len(matches),
			"matches":       matches,
		})
	}

	u := ui.FromContext(ctx)
	u.Out().Linef("correlation_id\t%s", correlationID)
	u.Out().Linef("rfc_message_id\t%s", rfcMessageID)
	u.Out().Linef("match_count\t%d", len(matches))
	for _, match := range matches {
		u.Out().Linef("message_id\t%s", match.MessageID)
		if match.ThreadID != "" {
			u.Out().Linef("thread_id\t%s", match.ThreadID)
		}
	}
	return nil
}

func validateGmailCorrelationID(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > gmailCorrelationMaxLen || value != strings.TrimSpace(value) || !gmailCorrelationPattern.MatchString(value) {
		return "", usagef("invalid --correlation-id: must be 1-%d opaque URL-safe ASCII characters", gmailCorrelationMaxLen)
	}
	return value, nil
}

func validateOpaqueReplyTo(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return usage("--correlation-id requires an exact --reply-to addr-spec")
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value {
		return usage("--correlation-id requires --reply-to to be one exact email address without a display name")
	}
	return nil
}

func requireExplicitGmailAccount(flags *RootFlags) (string, error) {
	if isADCAuthMode(flags) || hasDirectAccessToken(flags) {
		return "", usage("provider operations require account-bound stored credentials; ADC and direct access tokens are forbidden")
	}
	if flags == nil || flagAccount(flags) == "" || shouldAutoSelectAccount(flagAccount(flags)) {
		return "", usage("this provider operation requires an explicit --account; defaults and auto-selection are forbidden")
	}
	return requireAccount(flags)
}

func gmailCorrelationMessageID(correlationID string) (string, error) {
	correlationID, err := validateGmailCorrelationID(correlationID)
	if err != nil {
		return "", err
	}
	if correlationID == "" {
		return "", nil
	}
	digest := sha256.Sum256([]byte(correlationID))
	return fmt.Sprintf("<sportnet-%x@%s>", digest, gmailCorrelationDomain), nil
}

func gmailCorrelationHeaders(correlationID string) (map[string]string, error) {
	if correlationID == "" {
		return nil, nil
	}
	rfcMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		gmailCorrelationHeader: correlationID,
		"Message-ID":           rfcMessageID,
	}, nil
}

func verifySentGmailCorrelation(ctx context.Context, svc *gmail.Service, sent *gmail.Message, correlationID, replyTo string) (gmailCorrelationMatch, error) {
	if sent == nil || strings.TrimSpace(sent.Id) == "" {
		return gmailCorrelationMatch{}, fmt.Errorf("provider returned no message ID")
	}
	expectedRFCMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	msg, err := svc.Users.Messages.Get("me", sent.Id).
		Format(gmailFormatMetadata).
		MetadataHeaders("Message-ID", gmailCorrelationHeader, "Reply-To").
		Fields("id,threadId,payload(headers)").
		Context(ctx).
		Do()
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	match, err := verifiedGmailCorrelationMatch(msg, sent.Id, correlationID, expectedRFCMessageID)
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	if sent.ThreadId != "" && match.ThreadID != sent.ThreadId {
		return gmailCorrelationMatch{}, fmt.Errorf("provider thread ID changed from %q to %q", sent.ThreadId, match.ThreadID)
	}
	if match.ReplyTo != replyTo {
		return gmailCorrelationMatch{}, fmt.Errorf("provider Reply-To mismatch: got %q", match.ReplyTo)
	}
	return match, nil
}

func lookupSentGmailCorrelation(ctx context.Context, svc *gmail.Service, correlationID string) ([]gmailCorrelationMatch, string, error) {
	expectedRFCMessageID, err := gmailCorrelationMessageID(correlationID)
	if err != nil {
		return nil, "", err
	}
	query := "in:sent rfc822msgid:" + strings.Trim(expectedRFCMessageID, "<>")
	summaries, err := collectAllPages("", func(pageToken string) ([]*gmail.Message, string, error) {
		page, callErr := svc.Users.Messages.List("me").Q(query).MaxResults(100).
			PageToken(pageToken).Fields("messages(id,threadId),nextPageToken").Context(ctx).Do()
		if callErr != nil {
			return nil, "", callErr
		}
		return page.Messages, page.NextPageToken, nil
	})
	if err != nil {
		return nil, "", err
	}

	matches := make([]gmailCorrelationMatch, 0, len(summaries))
	seen := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary == nil || strings.TrimSpace(summary.Id) == "" {
			return nil, "", fmt.Errorf("provider lookup returned a result without a message ID")
		}
		if _, exists := seen[summary.Id]; exists {
			return nil, "", fmt.Errorf("provider lookup returned duplicate message ID %q", summary.Id)
		}
		seen[summary.Id] = struct{}{}
		msg, getErr := svc.Users.Messages.Get("me", summary.Id).
			Format(gmailFormatMetadata).
			MetadataHeaders("Message-ID", gmailCorrelationHeader, "Reply-To").
			Fields("id,threadId,payload(headers)").
			Context(ctx).
			Do()
		if getErr != nil {
			return nil, "", getErr
		}
		match, verifyErr := verifiedGmailCorrelationMatch(msg, summary.Id, correlationID, expectedRFCMessageID)
		if verifyErr != nil {
			return nil, "", verifyErr
		}
		if summary.ThreadId != "" && match.ThreadID != summary.ThreadId {
			return nil, "", fmt.Errorf("provider lookup thread mismatch for message %q", summary.Id)
		}
		matches = append(matches, match)
	}
	return matches, expectedRFCMessageID, nil
}

func verifiedGmailCorrelationMatch(msg *gmail.Message, requestedID, correlationID, expectedRFCMessageID string) (gmailCorrelationMatch, error) {
	if msg == nil || msg.Id != requestedID || strings.TrimSpace(msg.Id) == "" {
		return gmailCorrelationMatch{}, fmt.Errorf("provider returned malformed or mismatched message metadata")
	}
	observedCorrelationID, err := uniqueGmailIdentityHeader(msg.Payload, gmailCorrelationHeader)
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	observedRFCMessageID, err := uniqueGmailIdentityHeader(msg.Payload, "Message-ID")
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	replyTo, err := uniqueGmailIdentityHeader(msg.Payload, "Reply-To")
	if err != nil {
		return gmailCorrelationMatch{}, err
	}
	if err := validateOpaqueReplyTo(replyTo); err != nil {
		return gmailCorrelationMatch{}, err
	}
	if observedCorrelationID != correlationID {
		return gmailCorrelationMatch{}, fmt.Errorf("provider correlation mismatch for message %q", msg.Id)
	}
	if observedRFCMessageID != expectedRFCMessageID {
		return gmailCorrelationMatch{}, fmt.Errorf("provider RFC Message-ID mismatch for message %q", msg.Id)
	}
	return gmailCorrelationMatch{
		MessageID:     msg.Id,
		ThreadID:      msg.ThreadId,
		RFCMessageID:  observedRFCMessageID,
		CorrelationID: observedCorrelationID,
		ReplyTo:       replyTo,
	}, nil
}

// Identity headers must occur exactly once, including duplicates with equal values.
func uniqueGmailIdentityHeader(payload *gmail.MessagePart, name string) (string, error) {
	var value string
	count := 0
	if payload != nil {
		for _, header := range payload.Headers {
			if header != nil && strings.EqualFold(header.Name, name) {
				count++
				value = header.Value
			}
		}
	}
	if count != 1 || value == "" {
		return "", fmt.Errorf("provider requires exactly one %s header", name)
	}
	return value, nil
}
