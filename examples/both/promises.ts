const pipeline = async () => {
  const results = await Promise.all([
    runtime.run({
      name: "simple-task",
      image: "busybox",
      command: {
        path: "echo",
        args: ["Hello, World!"],
      },
    }),
    runtime.run({
      name: "simple-task",
      image: "busybox",
      command: {
        path: "echo",
        args: ["Hello, Bob!"],
      },
    }),
  ]);
  assert.containsString(results[0].stdout, "Hello, World!");
  assert.containsString(results[1].stdout, "Hello, Bob!");
};

export { pipeline };
