// Minimal benchmark pipeline - isolates container startup overhead
// Uses 'true' command which exits immediately with success
const pipeline = async () => {
  await runtime.run({
    name: "minimal",
    image: "busybox",
    command: {
      path: "true",
    },
  });
};

export { pipeline };
