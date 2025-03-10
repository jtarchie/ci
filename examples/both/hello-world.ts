const pipeline = async () => {
  let result = await runtime.run({
    name: "simple-task",
    image: "busybox",
    command: ["echo", "Hello, World!"],
  });
  assert.containsString("Hello, World!", result.stdout);

  result = await runtime.run({
    name: "show-env",
    image: "busybox",
    command: ["env"],
    env: {
      FOO: "bar",
    },
  });
  assert.containsString("FOO=bar", result.stdout);
};

export { pipeline };
