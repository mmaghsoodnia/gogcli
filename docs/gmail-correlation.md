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
