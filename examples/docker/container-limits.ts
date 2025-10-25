const pipeline = async () => {
  // Example 1: Set container limits with both CPU and memory
  let result = await runtime.run({
    name: "limited-resources",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Running with resource limits"],
    },
    container_limits: {
      cpu: 512, // CPU shares (0 means unlimited)
      memory: 134217728, // 128MB in bytes (0 means unlimited)
    },
  });
  assert.containsString(result.stdout, "Running with resource limits");

  // Example 2: Only CPU limit
  result = await runtime.run({
    name: "cpu-limited",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Running with CPU limit only"],
    },
    container_limits: {
      cpu: 256,
    },
  });
  assert.containsString(result.stdout, "Running with CPU limit only");

  // Example 3: Only memory limit
  result = await runtime.run({
    name: "memory-limited",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Running with memory limit only"],
    },
    container_limits: {
      memory: 67108864, // 64MB in bytes
    },
  });
  assert.containsString(result.stdout, "Running with memory limit only");

  // Example 4: No limits (unlimited resources)
  result = await runtime.run({
    name: "unlimited",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Running with unlimited resources"],
    },
  });
  assert.containsString(result.stdout, "Running with unlimited resources");
};

export { pipeline };
