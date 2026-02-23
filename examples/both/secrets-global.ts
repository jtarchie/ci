const pipeline = async () => {
  // This pipeline tests that global secrets are injected and redacted.
  // The test passes "global-secret-value-99999" as API_KEY via --global-secret.
  const result = await runtime.run({
    name: "use-global-secret",
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
  assert.containsString(result.stdout, "my-secret-value=");

  // Verify the actual global secret value is NOT in the output
  assert.truthy(
    !result.stdout.includes("global-secret-value-99999"),
    "global secret value should be redacted from output",
  );
};

export { pipeline };
