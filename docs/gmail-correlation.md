# Exact Gmail correlation for durable send reconciliation

`gog gmail send --correlation-id` is a narrow provider contract for a durable
outbox. It is designed for callers that must distinguish a failed send from a
send whose provider response or local commit was lost.

## Contract

A correlated send requires both an explicit `--account` and one exact
addr-spec in `--reply-to`. Defaults, `--account auto`, display-name Reply-To
values, malformed header values, and split tracking sends are rejected before
the send API is called.

The CLI writes the opaque value to `X-Sportnet-Correlation-ID` and derives a
stable, non-reversible RFC Message-ID from it. Before sending, it performs an
exact sent-mail lookup for that stable Message-ID. One existing provider copy
is treated as a duplicate retry and multiple copies are treated as ambiguous;
both refuse the send.

After Gmail accepts the message, the CLI fetches that exact Gmail message ID
in metadata format and verifies the provider copy's:

- Gmail message ID and thread ID;
- RFC `Message-ID`;
- `X-Sportnet-Correlation-ID`; and
- `Reply-To`.

Only then does JSON output include `messageId`, `threadId`, `rfcMessageId`,
`correlationId`, and `replyTo`. If verification fails after Gmail acceptance,
the command reports an uncertain outcome and tells the caller not to retry.

## Exact reconciliation

Use the dedicated command, always with the same explicit account:

```bash
gog --account advocate@sportnet.ai --json --no-input \
  gmail messages lookup-correlation sportnet-OPAQUE_VALUE
```

The command derives the same stable RFC Message-ID, uses Gmail's exact
`rfc822msgid:` sent-mail query, fetches every candidate by provider message ID,
and re-verifies the custom correlation header. It returns:

```json
{
  "correlationId": "sportnet-OPAQUE_VALUE",
  "rfcMessageId": "<sportnet-HASH@correlation.sportnet.ai>",
  "matchCount": 1,
  "matches": [
    {
      "messageId": "provider-id",
      "threadId": "provider-thread-id",
      "rfcMessageId": "<sportnet-HASH@correlation.sportnet.ai>",
      "correlationId": "sportnet-OPAQUE_VALUE",
      "replyTo": "reply+OPAQUE@reply.sportnet.ai"
    }
  ]
}
```

`matchCount` and `matches` preserve zero, one, and multiple outcomes. Missing,
duplicate, cross-correlation, or otherwise malformed provider evidence fails
closed. The lookup is stateless and remains usable after a CLI or MCP restart;
it does not rely on a local idempotency registry.

## Rollout gate

Do not validate this contract by sending external mail. Keep provider changes
on an isolated branch until the gog MCP wrapper and this CLI build are tested
together against an isolated mock or an explicitly authorized non-delivering
provider canary. A production Gmail canary, shared service deployment, or
consumer activation requires separate explicit authorization.

## Reconciliation read hardening

Identity verification requires exactly one Message-ID, X-Sportnet-Correlation-ID
and Reply-To header, counting names case-insensitively. Repeated headers are
rejected even if equal. Every fetched provider ID must equal the requested ID.
Correlation lookup follows all pages and rejects token cycles and duplicate IDs;
an error on any page or metadata read fails the entire lookup.

For exact absence evidence use
`gog --account advocate@sportnet.ai --json gmail get ID --not-found-result`.
Only a Google API 404 from that message GET returns
`{"status":"not_found","account":"advocate@sportnet.ai","messageId":"ID"}`.
Other errors remain failures; callers must validate both account and ID. The flag
requires JSON and an explicit account. Without the flag, existing error behavior
is retained. Search JSON always includes nextPageToken; an empty string denotes
end-of-results, not a snapshot guarantee.

Correlation preflight detects visible duplicates; it is not provider-enforced
idempotency. Concurrent calls and Gmail indexing delays can still produce
duplicates. A zero-match lookup following an ambiguous send is not proof that
resending is safe. Serialize sends per correlation and keep uncertain operations
pending reconciliation. Correlated POSTs disable automatic transport retries;
ADC and direct access tokens are rejected because they do not bind credentials
to the requested account.
