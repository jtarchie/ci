const pipeline = async () => {
  let result = await runtime.run({
    name: "simple-task",
    image: "busybox",
    command: ["echo", "Hello, World!"],
  });
  assert.containsString(result.stdout, "Hello, World!");

  result = await runtime.run({
    name: "show-env",
    image: "busybox",
    command: ["env"],
    env: {
      FOO: "bar",
    },
  });
  assert.containsString(result.stdout, "FOO=bar");
};

export { pipeline };
