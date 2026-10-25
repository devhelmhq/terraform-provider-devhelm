# Changelog

## Unreleased

### Added

- `devhelm_monitor` supports `BROWSER` and `MULTI_STEP_API` code monitors with a
  `code` block (`entrypoint`, `include` for fixtures and data that ship without
  being imported, `root`). Plan uploads the include set
  (`POST /bundles`) and computes `code_hash` and `code_files`; apply validates
  new monitors before creating them and publishes changed code through
  draft → test → publish (`test_before_publish`, default true).
- Computed `code_hash`, `code_files`, `live_revision_id`, `live_revision_digest`
  and `owner`; settings `capture_policy`, `fast_retry_max_attempts`,
  `run_parallel`, `secret_keys`, `acknowledge_rollback`, `owner_enforced`,
  `owner_address` and `timeouts`.
- `devhelm_secret_value`: a secret's value in one environment, write-only
  (Terraform 1.11+), with a value stamp in state.
- Data sources `devhelm_runtime` and `devhelm_monitor_revision`.
- Every request sends `X-DevHelm-Client: terraform/<version>`; every POST sends
  an `Idempotency-Key` reused on retries. 429/503 and retryable 409s are retried
  up to 5 times, honouring `Retry-After`.
- Diagnostics read `[<category>] <code>: <message>`, with `next.command`, docs and
  the request id.

### Changed

- New monitors are created with `ownerEnforced = true`. Use an API key created
  with tool `TERRAFORM`; another key's later writes are refused with an
  `[ownership]` diagnostic.
- `terraform import devhelm_monitor` reads the monitor; the next apply transfers
  ownership to Terraform (the plan shows `owner` changing).
- `config` is optional for code monitors; `frequency_seconds` accepts 10–86400
  (the minimum depends on the plan).

### Removed

- `devhelm_monitor.enabled`. Pause and mute are overlays: use
  `devhelm monitors pause|resume|mute|unmute <monitor>`. Setting `enabled` is
  an error; existing state clears on refresh.
