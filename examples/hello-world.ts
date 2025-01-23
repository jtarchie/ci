const pipeline = () => {
  const result = run({
    name: "simple-task",
    image: "alpine",
    command: ["echo", "Hello, World!"],
  });
  console.log(JSON.stringify(result, null, 2));
};

export { pipeline };
