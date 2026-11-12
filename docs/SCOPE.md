# API scope and verification

## What “complete” means here

The generic `api operations`, `api describe`, and `api call` commands cover every operation in the bundled `openapi.json`: **157 HTTP method/path operations**. Friendly commands are shortcuts for common support workflows, not a separate implementation of all 157 operations.

The document includes application/account APIs, client/public APIs, platform administration, reporting, and additional documented routes. The embedded snapshot is the authority for operation names, methods, paths, parameters, and request schemas. Runtime permissions are enforced by the Chatwoot server.

This scope deliberately does not promise:

- Every route in Chatwoot's Rails application, private dashboard APIs, enterprise-only internals, or future endpoints.
- Availability on every hosted or self-hosted version.
- An authenticated end-to-end test of all operations.
- Client-side enforcement of every JSON Schema constraint.
- Automatic pagination for arbitrary generic or raw API calls.

`api raw` is the explicit escape hatch for routes outside the snapshot. Use it only with documentation and authorization appropriate to your server.

## Upstream source

The specification is merged from the official Chatwoot repository's `develop` API documentation. Upstream metadata identifies the API document as version `1.1.0`; this is **not** the Chatwoot server release version. Development documentation can describe behavior that is not yet present in a deployed release.

Reference sources:

- https://github.com/chatwoot/chatwoot/tree/develop/swagger
- https://developers.chatwoot.com/api-reference/introduction
- The exact local snapshot: [`../openapi.json`](../openapi.json)

To independently count method/path operations and identify this exact artifact:

```sh
python3 - <<'PY'
import hashlib, json
from pathlib import Path
p = Path('openapi.json')
raw = p.read_bytes()
spec = json.loads(raw)
methods = {'get', 'post', 'put', 'patch', 'delete', 'head', 'options', 'trace'}
ops = [(method, path, op.get('operationId'))
       for path, item in spec['paths'].items()
       for method, op in item.items() if method in methods]
print('operations:', len(ops))
print('unique operation IDs:', len({op[2] for op in ops}))
print('sha256:', hashlib.sha256(raw).hexdigest())
PY
```

The delivered snapshot SHA-256 is `51c3ac66d5c1959337167f553ca3be0407a239ac5f32674ae6aea6add348f425` (157 operations).

Use the local artifact/hash when reproducing a result; a `develop` URL is mutable. Refreshing the specification can change the operation count and CLI-visible operation IDs, and should be followed by tests and review.

## Verification boundaries

The delivered automated tests use local HTTP fixtures to inspect actual request construction and response handling without contacting a production account. These tests establish behavior only for their covered scenarios. They do not prove that every upstream operation is valid on a live deployment.

No live Chatwoot credential test or production write was performed for this deliverable. Before adoption, build and run the tests, then test read-only calls against your own server. Test writes separately in a disposable account using an appropriately scoped credential. Review your deployment's authentication mode, account membership, platform permissions, pagination, attachment limits, and rate limits.

## Known upstream specification anomalies

The catalog preserves upstream spelling and routing. One operation (`getConversationMessages`) uses `/accounts/{account_id}/conversations/{conversation_id}/messages` without `/api/v1`, an apparent upstream documentation defect. `api call` explicitly normalizes this one operation to `/api/v1/accounts/.../messages`, the same canonical route used by `messages` and `get`. The source catalog remains unchanged for traceability. `api raw` never applies this correction. Reporting routes that differ by a trailing slash are preserved, not collapsed. Neither normalization nor fixture tests constitute live deployment acceptance.

Path-item parameters inherited by an operation are exposed as `operation.spec.x-path-parameters` by `api describe`; referenced schemas and parameters are supplied in `components`.

Source Git blob identifier: `071dbc4a27dadbeafcc59566626d574d8a22363c`. Retrieved 2026-11-11. Machine-readable command mapping: [`api-coverage.json`](api-coverage.json).
