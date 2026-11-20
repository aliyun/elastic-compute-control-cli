# E2E Agent Guidelines

These instructions apply to automated tests and test fixtures under e2e/ in the configured dedicated test account.

## Test resource identity and authorization

- The user has confirmed that this account is a test account and accepts the operations required by these tests. Run the approved tests and fixture cleanup without requesting repeated risk acceptance.
- An OSS Bucket name is the unique identity of that Bucket in this test environment. Use the configured or run-assigned name for creation, lookup, reuse and cleanup.
- Test RAM fixtures are identified by their configured or run-assigned names. Do not block tests on hypothetical RAM or Bucket name reuse, an unavailable immutable instance/generation identifier, or an additional uniqueness/permission approval.
- Record the test profile, region and fixture names so execution and cleanup address the intended test targets. Actual API errors are test results to diagnose and fix.
- Sandbox resource tests use cn-hangzhou. Follow the current execution channel and run-specific cleanup records for the test operation.

## Test verification

- Keep assertions tied to the intended named fixture or captured resource ID. Preserve real command exit codes, business-status checks and observed test results.
- Run E2E and ecctl binary execution on the authorized daily Linux environment. Keep the configured credentials on that host.
