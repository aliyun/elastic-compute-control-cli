# E2E operation coverage

Resource coverage and operation coverage are different gates. A `list` smoke
case proves that a resource command is reachable, but it does not cover the
resource's `create`, `update`, `delete`, or protected actions.

The operation audit found 66 gaps across 20 resources: 15 missing `get`
operations and 51 missing mutation/action operations. The cause was the old
completion rule, which required only one case per implemented resource. The
suite now requires both:

```bash
make coverage-resources
make coverage-operations
```

The public capability-filtered spec-to-case gate has zero gaps. A case being
present is not live evidence: every new or changed operation remains `offline`
until the exact case fingerprint passes against Alibaba Cloud.

## Gap closure and prerequisite model

| Resource | Former gaps | Why list/get-only coverage was insufficient | Case and cleanup model |
|---|---|---|---|
| `ack/addon` | `create`, `get`, `update`, `upgrade`, `delete` | The catalog list was used only to remove the resource gap even though ACK exposes the complete addon lifecycle. | Dynamically select a non-default, unreserved addon whose catalog metadata supports install/modify/upgrade/uninstall and an empty config, then install/read/update/upgrade/uninstall it on the disposable cluster. |
| `ack/check` | `create`, `get` | A check is an immutable report task, not a list-only resource. | Run `ClusterUpgrade` check on the disposable cluster and read it back. There is no delete API; cluster deletion owns its lifetime. |
| `ack/policy instance` | `create`, `get`, `update`, `delete` | The provider-generated instance name was not projected from create, so cleanup could not be registered safely. | Create a warn-only `ACKAllowedRepos` instance, capture the returned instance name, update/read it, and delete it. |
| `ack/inspect report` | `create`, `get` | Listing reports did not exercise `RunClusterInspect`. | Trigger and read a report on the disposable cluster. Reports have no delete API and disappear with the cluster. |
| `ack/task` | `get`, `pause`, `resume`, `cancel` | ACK tasks are created by other workflows and not by the task resource. | The destructive ACK lifecycle starts a nodepool upgrade with `--no-wait`, captures its task ID, exercises pause/resume/cancel, and then restores the nodepool lifecycle on the same disposable cluster. |
| `ack/vuls` | `create` | Listing vulnerability results did not exercise scan creation. | Trigger a scan on the disposable cluster and read the resulting view. Scan history has no delete API. |
| `lingjun/eni` | `create`, `get`, `update`, `delete` | Attach/detach depends on a physical node and is not part of the public surface; inventory-only coverage would not verify ENI mutations. | Create a tagged ENI in the reviewed network, read and update it, then delete it. |
| `lingjun/er` | `create`, `get`, `update`, `delete` | The former case checked only account inventory. | Create a tagged ER in the reviewed Lingjun zone, update/read it, and delete it. |
| `lingjun/net-test` | `create`, `get` | A valid test needs two dedicated running nodes. | `lingjun.net_test` supplies the cluster and nodes. The task result is bounded history because no delete API exists. |
| `lingjun/subnet` | `create`, `get`, `update`, `delete` | The former case lacked a fixture-owned VPD and reviewed zone. | The stack creates a shared tagged VPD; the case creates, updates, reads, and deletes its tagged subnet. |
| `rg/admin-setting` | `update` | This is an account-global singleton and cannot be treated as disposable. | Capture the original boolean, exercise both values, and restore the captured value in teardown. |
| `rg/associated-transfer` | `enable`, `update`, `disable` | This is an account-global singleton. | Preflight accepts `rg.associated_transfer_disabled` only when the initial state is disabled; the final action restores disabled. |
| `rg/notification` | `enable`, `disable` | This is an account-global singleton. | Preflight accepts `rg.notification_disabled` only when the initial state is false; the final action restores false. |
| `tag/associated-resource-rule` | `create`, `update`, `delete` | `SettingName` is a provider-defined rule name that is unique per account and region. | The case clears any existing rule first with `not_found_ok` (unique resources are deleted before create), then creates/updates/lists/deletes its own rule. |
| `tag/policy` | `create`, `get`, `update`, `attach`, `detach`, `delete` | The former case stopped at list even though single-account attach/detach needs no external target ID. | Create a disposable policy, read/update it, attach/detach in single-account mode, and delete it. |

## Safety rule

Creatable dependencies belong in `fixtures/stack.yaml`. A protected
prerequisite is allowed only for inventory or state that ecctl cannot create
and safely remove, such as a physical Lingjun node or a reviewed account-global
initial state. Missing or
unsafe prerequisites skip before mutation; they do not turn an operation into
`live-pass`.
