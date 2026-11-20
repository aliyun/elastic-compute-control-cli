//go:build darwin && !cgo

package main

// The E2E harness must inspect and clear ACLs through opened file descriptors.
// Darwin builds require the native cgo implementation for that custody check.
var _ = DarwinE2EHarnessRequiresCGOEnabled1ForPrivateACLValidation
