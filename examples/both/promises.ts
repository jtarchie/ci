const pipeline = async () => {
  const results = await Promise.all([
    runtime.run({
      name: "simple-task",
      image: "busybox",
      command: ["echo", "Hello, World!"],
    }),
    runtime.run({
      name: "simple-task",
      image: "busybox",
      command: ["echo", "Hello, Bob!"],
    }),
  ]);
  assert.containsString("Hello, World!", results[0].stdout);
  assert.containsString("Hello, Bob!", results[1].stdout);
};

export { pipeline };
