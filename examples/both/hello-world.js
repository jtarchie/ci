// This is an example to show the Javascript is supported.
// It would be preferred to use Typescript for better type safety.
// There will not be more examples in Javascript beyond this.

const pipeline = async () => {
  const result = await runtime.run({
    name: "simple-task",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Hello, World!"],
    },
  });
  assert.containsString(result.stdout, "Hello, World!");
};

export { pipeline };
