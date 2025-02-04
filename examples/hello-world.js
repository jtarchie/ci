const pipeline = async () => {
  const result = await run({
    name: "simple-task",
    image: "alpine",
    command: ["echo", "Hello, World!"],
  });
  assert.containsString("Hello, World!", result.stdout);
};

export { pipeline };
