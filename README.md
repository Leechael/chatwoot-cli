# Chatwoot CLI

An independent Go command-line client for Chatwoot. It combines convenient support-inbox commands with a generic interface to every operation in the bundled official OpenAPI specification. It is not an official Chatwoot product and does not require Phala.

**Scope:** 157 documented operations in this snapshot, covering the application, client/public, and platform APIs. This is complete coverage of the bundled specification, not a claim that every undocumented Chatwoot route is supported or that every operation has been exercised against a live server. See [scope and provenance](docs/SCOPE.md).

## Build and verify

```sh
go build -o chatwoot-cli .
go test ./...
./chatwoot-cli --help
```

Tests use local HTTP fixtures. No production credentials are included, and live authenticated calls or production writes have not been performed as part of this deliverable. All network commands use real HTTP; there is no fabricated server data or simulated success mode.

## Configuration

Set your server's origin, without an API path, and your account ID:

```sh
export CHATWOOT_BASE_URL='https://chatwoot.example.com'
export CHATWOOT_ACCOUNT_ID='1'
# Inject CHATWOOT_API_TOKEN from your secret manager or protected environment.
./chatwoot-cli status
./chatwoot-cli list --status open --page 1 --json
```

Use HTTPS outside local development. Do not paste tokens into committed files, issue reports, chat transcripts, shell history, or screenshots. Environment variables are preferable to command-line token flags because process listings and command history can expose flags. Environment variables are still accessible to sufficiently privileged processes, so use a restricted runtime and a secret manager where possible.

| Flag | Environment / purpose |
| --- | --- |
| `--base-url` | `CHATWOOT_BASE_URL`, Chatwoot server origin |
| `--token` | `CHATWOOT_API_TOKEN`, falling back to `CHATWOOT_AGENT_API_TOKEN` |
| `--platform-token` | `CHATWOOT_PLATFORM_TOKEN`, platform application credential |
| `--account-id` | `CHATWOOT_ACCOUNT_ID`, default account for account-scoped calls |
| `--auth-mode api-key\|bearer` | Select the API access token header (default) or Bearer authorization |
| `--timeout` | HTTP request timeout, default `30s` |
| `--json` | Compact JSON output for scripts; default output is indented JSON |
| `--plain` | Headerless TSV: JSON field path and JSON-encoded scalar |
| `--jq FILTER` | In-process gojq expression; requires `--json` |

`--json` and `--plain` are mutually exclusive. For example, `./chatwoot-cli api operations --json --jq '.[].operationId'` prints operation IDs as JSON strings. Jq filtering runs in-process through gojq; a separate `jq` executable is not required.

Exit codes are `0` for success, `1` for validation/transport/server errors, `2` for HTTP 401/403, and `3` for HTTP 404. Run `./chatwoot-cli exit-codes` for the same list. Errors go to stderr; successful results go to stdout. Empty successful responses are represented by an object containing the HTTP status. Successful non-JSON responses are returned as an envelope with `status`, `content_type`, and `body` fields.

Configuration profiles are environment-based. There is no profile database or login credential store. Use separate shells, your secret manager's environment injection, or protected environment files for staging and production. Never commit those files.

### Credential families

- **Application API:** Use a user's access token from Chatwoot Profile Settings for `/api/...` account and profile endpoints. Account membership and role still control access. Agent bot tokens are limited to APIs that Chatwoot permits for bots; they are not interchangeable with an administrator token.
- **Client/public API:** `/public/api/...` endpoints use the inbox identifier and, where relevant, contact identifier in their paths. These are different from numeric account/contact IDs. Obtain the identifiers from your Chatwoot integration and keep contact-specific identifiers private. Public API access is not a substitute for authenticated application access.
- **Platform API:** `/platform/api/...` endpoints require a platform app token created through the self-hosted Super Admin interface. Set `CHATWOOT_PLATFORM_TOKEN`; an ordinary account token does not grant platform administration rights. Availability and resource access depend on the server's deployment and platform app permissions.

The bundled upstream spec documents Bearer authentication as available in Chatwoot **v4.19.0 and later**. Choose `--auth-mode bearer` on a compatible server. The `api_access_token` header remains supported in the spec for older deployments. Match your server version rather than assuming the development specification matches your installation.

## Common support workflows

Read-only examples:

```sh
./chatwoot-cli status
./chatwoot-cli list --status open --inbox-id 2 --assignee-type me --all
./chatwoot-cli get 123 --json
./chatwoot-cli messages 123 --all --json
./chatwoot-cli messages 123 --before 456
./chatwoot-cli search 'customer@example.com'
./chatwoot-cli integrations
./chatwoot-cli list-members
./chatwoot-cli contacts
```

`ls` is an alias for `list`. `search QUERY` searches contacts, not conversation messages; `contacts` returns the first contacts page. For further contact pages, use `api call contactList --query page=N`. `get` retrieves the conversation and full available message timeline. `messages --all` walks message history; `--before` supplies a message-history cursor. `list --page N` selects a page, while `list --all` walks pages. Pagination is based on the server's responses; concurrent inserts or deletions can change results while pages are being read. Large inboxes and histories require multiple requests and may encounter server rate limits.

The commands below **change server state**. Run them only with the appropriate authorization and against the intended account. They are examples, not a dry-run workflow:

```sh
./chatwoot-cli reply 123 'Thanks. We are checking this for you.'
printf '%s' 'Internal investigation notes.' | ./chatwoot-cli reply 123 --internal
./chatwoot-cli reply 123 'Attached is the requested file.' --attachment ./receipt.pdf
./chatwoot-cli reply 123 'Two files attached.' --attachment ./a.pdf --attachment ./b.pdf
./chatwoot-cli update-status 123 resolved
./chatwoot-cli assign 123 7 --team-id 2
```

`assign` accepts either an agent ID or a case-insensitive agent name. Prefer an ID for automation to avoid name ambiguity.

An internal reply is a private note, not a customer-visible outgoing reply. Attachments are uploaded to your Chatwoot server and may become available to the conversation's recipients. Verify the target conversation and contents first. Use a test account for write testing.

## Entire documented API

Find an operation, inspect its parameters, then call it by its exact, case-sensitive operation ID:

```sh
./chatwoot-cli api operations
./chatwoot-cli api operations conversation
./chatwoot-cli api describe get-details-of-a-conversation
./chatwoot-cli api call get-details-of-a-conversation --param conversation_id=123
./chatwoot-cli api call contactList --query page=1
./chatwoot-cli api call get-account-agents
./chatwoot-cli api call get-details-of-a-inbox --param inbox_identifier=YOUR_INBOX_IDENTIFIER
./chatwoot-cli api call list-all-agent-bots
```

`--param key=value` substitutes a named path parameter. Account-scoped operations use the configured account ID. `--query key=value` supplies a query parameter. Generic calls do not automatically traverse every endpoint's pagination: use the operation's documented page or cursor parameters.

Request bodies can come from an inline JSON value, a local file prefixed with `@`, or standard input represented by `-`. Multipart forms support repeated `--form` and `--file` flags. For example, these commands perform writes:

```sh
./chatwoot-cli api call toggle-status-of-a-conversation \
  --param conversation_id=123 --data '{"status":"resolved"}'
./chatwoot-cli api call create-a-new-message-in-a-conversation \
  --param conversation_id=123 --data @message.json
printf '%s' '{"content":"Private note","private":true}' | \
  ./chatwoot-cli api call create-a-new-message-in-a-conversation \
  --param conversation_id=123 --data -
./chatwoot-cli api call create-a-new-message-in-a-conversation \
  --param conversation_id=123 --form 'content=Attached file' \
  --form 'message_type=outgoing' --file 'attachments[]=./receipt.pdf'
```

Read `api describe OPERATION_ID` and the relevant server documentation before sending a body. The CLI is not a replacement for Chatwoot's server-side authorization or validation, and spec coverage does not establish that a payload is valid for every Chatwoot release.

### Raw escape hatch

Use an explicit method and server-relative path for a newer or undocumented route:

```sh
./chatwoot-cli api raw GET /api/v1/profile
./chatwoot-cli api raw GET /api/v1/accounts/1/conversations --query status=open --query page=1
```

`api raw` accepts the same query/body/form/file options. Supply actual IDs in its path. Raw access does not grant extra server permissions, make an undocumented route stable, or provide automatic pagination. Do not put secrets in paths or query strings.

## Automation and optional Phala use

The CLI can run as a normal subprocess in a local script, CI job, or a Phala-hosted service. Phala is optional: no Phala API, SDK, wallet, attestation, or deployment setup is required for ordinary use. To integrate it into an existing service, inject the Chatwoot environment variables through that service's secret store, use JSON output, check process exit status, and scope the credential to the accounts and operations the job needs. Deploying to Phala or configuring its secrets is outside this repository's verified scope.

Do not automatically retry a failed or interrupted write without checking its outcome. A server may have accepted a message even if the response was lost. Respect rate limits and avoid logging full conversation bodies, attachment contents, credentials, or customer identifiers.

## Reference

- [Chatwoot API documentation](https://developers.chatwoot.com/api-reference/introduction)
- [Chatwoot source repository](https://github.com/chatwoot/chatwoot)
- [Scope, reproducibility, and limitations](docs/SCOPE.md)
- [Bundled OpenAPI document](openapi.json)

## Real deployment acceptance

Go 1.24 or newer is required. Build outputs can be created with `make release`; release naming is centralized in `release-naming.env`. The ZIP includes binaries for macOS arm64/amd64, Linux arm64/amd64 and Windows amd64. Binaries are unsigned; macOS may require the normal user-approved security handling for downloaded unsigned tools.

Run `go test -tags integration -run TestLiveReadOnly -v` with `CHATWOOT_BASE_URL`, `CHATWOOT_API_TOKEN` and `CHATWOOT_ACCOUNT_ID` to check profile and conversation reads. Optionally set `CHATWOOT_TEST_CONVERSATION_ID` to verify full history. This test skips when credentials are missing and never performs writes. Ordinary unit tests are HTTP contract fixtures, not a fake runtime implementation.

Design references: [go-api-cli-playbook](https://github.com/Leechael/go-api-cli-playbook) and [mava-cli](https://github.com/Leechael/mava-cli). This POC adopts machine-readable output, stable exit codes, operation inventory, ticket shortcuts, CI checks and release naming. It does not claim full adoption of their optional release-comment automation or BDD framework.
