# CI

<!-- deno-fmt-ignore-start -->
> [!IMPORTANT]
> This is a work in progress. It will change.
<!-- deno-fmt-ignore-end -->

The motivation of this project is to be able to automate some work flows. I was
doing it was (bad) bash script, but kept running into edge conditions. I'm
trying to create a runtime similar to [Concourse CI](https://concourse-ci.org/),
but runs container platforms -- docker, docker swarm, and fly.io.

## Testing

This is relying on strict integration testing at the moment. I'd like to keep
the interfaces the same, but change underlying implementation.

Right now, only the platforms of `docker` and `native` are tested against.
Primarily because `fly.io` requires a cost, eventually it will be added.

```bash
brew bundle
task
```
