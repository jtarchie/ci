const pipeline = async () => {
  const results = await Promise.all([
    run({
      name: "simple-task",
      image: "alpine",
      command: ["echo", "Hello, World!"],
    }),
    run({
      name: "simple-task",
      image: "alpine",
      command: ["echo", "Hello, Bob!"],
    }),
  ]);
  assert.containsString("Hello, World!", results[0].stdout);
  assert.containsString("Hello, Bob!", results[1].stdout);
};

export { pipeline };
