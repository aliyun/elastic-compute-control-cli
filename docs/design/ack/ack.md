# ACK Default Cluster Commands

`ecctl ack` manages ACK clusters as the product default resource. It mirrors the cluster-focused design in `cluster.md` while keeping the common `ecctl ack <action>` form available for high-frequency cluster workflows.

## `ecctl ack create`

Create an ACK cluster from the required cluster configuration flags and optional advanced request parameters.

## `ecctl ack update`

Update mutable ACK cluster attributes such as name, API server exposure, tags, or maintenance window.

## `ecctl ack delete`

Delete an ACK cluster by cluster ID, using the same safety confirmation conventions as other destructive resource commands.

## `ecctl ack get`

Get one ACK cluster by cluster ID, with optional related-detail expansion flags.

## `ecctl ack list`

List ACK clusters in the selected region, with supported filters and pagination.

## `ecctl ack upgrade`

Start an ACK cluster upgrade for the requested Kubernetes version or upgrade plan.
