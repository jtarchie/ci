const pipeline = async () => {
  // This pipeline tests that secrets are injected via the "secret:" prefix
  // and that secret values are redacted from output.
  const result = await runtime.run({
    name: "use-secret",
    image: "busybox",
    command: {
      path: "sh",
      args: ["-c", "echo my-secret-value=$API_KEY"],
    },
    env: {
      API_KEY: "secret:API_KEY",
    },
  });

  // The secret value should NOT appear in stdout (it should be redacted)
  // Use containsString with regex pattern that matches the redacted marker
  assert.containsString(result.stdout, "my-secret-value=");

  // Verify the actual secret value is NOT in the output (it's been redacted)
  assert.truthy(
    !result.stdout.includes("super-secret-value-12345"),
    "secret value should be redacted from output",
  );
};

export { pipeline };
