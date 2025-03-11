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
  assert.containsString(results[0].stdout, "Hello, World!");
  assert.containsString(results[1].stdout, "Hello, Bob!");
};

export { pipeline };
