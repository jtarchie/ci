# Backward compatibility

This package comes out of the want/need to map
[Concourse CI](https://concourse-ci.org) pipelines to underlying runtime. First,
it helps drive out features, edges, and the footprint of the runtime. Secondly,
the YAML config of a pipeline is already familiar to a lot of people.

## Anti Goals

We cannot support everything. Recreating concourse would be a huge undertaking.

Here are list (non-exhaustive) list of things that won't be supported at the
moment:

- Volume management for overlay/btrfs volumes. We are relying on the underlying
  runtimes (docker, fly, etc) for their volume support.
- Tasks across many workers (Mac, special networking, etc) in one job.
  Orchestration of one runtime in one _job_ execution will only be support.
- Secret management. Will remain stateless.
