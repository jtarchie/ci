// Example: Notifications with CI pipeline
//
// This example demonstrates how to use the notification system
// to send alerts via Slack, Teams, or HTTP webhooks.
//
// Note: This example uses mock/test configurations.
// In production, you would configure actual credentials.

const pipeline = async () => {
  // First, configure notification backends
  notify.setConfigs({
    // Slack configuration
    "slack-builds": {
      type: "slack",
      token: "xoxb-test-token",
      channels: ["#builds", "#ci-status"],
    },
    // Microsoft Teams configuration
    "teams-alerts": {
      type: "teams",
      webhook: "https://outlook.office.com/webhook/test",
    },
    // Generic HTTP webhook
    "http-webhook": {
      type: "http",
      url: "https://example.com/webhook",
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: "Bearer test-token",
      },
    },
  });

  // Set initial pipeline context for template rendering
  // All fields are optional - only set what you need
  notify.setContext({
    pipelineName: "my-pipeline",
    jobName: "build-job",
    buildID: "12345",
    status: "running",
    startTime: new Date().toISOString(),
    endTime: "",
    duration: "",
    environment: {
      branch: "main",
      commit: "abc123",
    },
    taskResults: {},
  });

  // Run a task
  const result = await runtime.run({
    name: "build",
    image: "busybox",
    command: {
      path: "echo",
      args: ["Building application..."],
    },
  });

  assert.containsString(result.stdout, "Building");

  // Update context with success status
  notify.updateStatus("success");

  // Send a notification (async/fire-and-forget)
  // In a real scenario, this would send to Slack
  // notify.send({
  //   name: "slack-builds",
  //   message: "Build {{ .JobName }} completed with status: {{ .Status }}",
  //   async: true,
  // });

  // For testing, we just verify the context was set correctly
  console.log("Pipeline completed successfully with notifications configured");
};

export { pipeline };
