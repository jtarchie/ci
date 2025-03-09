# Examples

These are examples that show the progress of the `ci` system. All examples work
because all examples are tests.

- In JS/TS examples, the are `assert` statements.
- In the YAML examples, a new property ()`assert`) has been added for `task` to
  ensure the output is correct.

All the examples currently run under [`examples_test.go`](../examples_test.go)
to ensure the assertions are run with any code changes.

## TS type definition

A separate package (`@jtarchie/ci`) is being maintained [here](../packages/ci).
The purpose to ensure that type safety for runtime can be maintained. Please
keep in mind that CI will not check types, that is left for your favorite editor
to ensure for you.
