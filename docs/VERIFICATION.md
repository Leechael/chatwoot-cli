# Verification report

2026-11-11, Go 1.24.2 linux/amd64.

- `go test -race -cover ./...`: PASS; 56.9% statement coverage.
- `go vet ./...`: PASS.
- Embedded operation catalog: 157 unique IDs, 96 paths. Every method/path traverses the HTTP fixture transport/auth contract test.
- Contract tests cover platform/application/public token separation, legacy/Bearer headers, redirects, JSON arbitrary fields, multipart bytes and private flag, error exit codes, percent-encoded paths/query values, message history pagination/order/dedup/no-progress, 64 MiB overflow, upstream route correction, inherited path parameters, conversation ID validation, reply command and generic CLI path/query substitution, output mode validation.
- CLI smoke checks: operation count is 157; in-process `--json --jq length` is 157; help renders.
- Builds: macOS arm64/amd64, Linux arm64/amd64, Windows amd64. Cross-compilation does not mean execution on those operating systems. Linux amd64 binary smoke-tested.
- `go test -tags integration -run TestLiveReadOnly -v`: SKIPPED because credentials were not supplied. No authenticated live tests and no production writes performed.

The tests are local HTTP contract fixtures, not deployment mocks or fabricated runtime data. This report is not proof that all upstream APIs work on a particular Chatwoot deployment. Run the provided read-only integration test with your own configuration, then verify writes in an authorized disposable/test conversation.
