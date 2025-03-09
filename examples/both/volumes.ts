const pipeline = async () => {
  const volume = await runtime.createVolume();
  let result = await runtime.run({
    name: "simple-task",
    image: "alpine",
    command: ["sh", "-c", "echo Hello, World! > ./mounted-volume/hello.txt"],
    mounts: {
      "mounted-volume": volume,
    },
  });
  console.log(JSON.stringify(result));
  assert.equal(result.code, 0);

  result = await runtime.run({
    name: "simple-task",
    image: "alpine",
    command: ["cat", "./mounted-volume/hello.txt"],
    mounts: {
      "mounted-volume": volume,
    },
  });
  console.log(JSON.stringify(result));
  assert.equal(result.code, 0);
  assert.containsString("Hello, World!", result.stdout);
};

export { pipeline };
