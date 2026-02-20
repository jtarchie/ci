// GitHub webhook handler - inspects event type from headers
const pipeline = () => {
  const req = http.request();

  if (!req) {
    console.log("Not triggered via webhook, skipping");
    return;
  }

  const event = req.headers["X-Github-Event"] || req.headers["x-github-event"];
  const delivery = req.headers["X-Github-Delivery"] ||
    req.headers["x-github-delivery"];

  console.log(`GitHub event: ${event}, delivery: ${delivery}`);

  // Acknowledge immediately
  http.respond({
    status: 200,
    body: JSON.stringify({ accepted: true, event }),
    headers: { "Content-Type": "application/json" },
  });

  // Parse payload and process based on event type
  let payload: Record<string, unknown> = {};

  try {
    payload = JSON.parse(req.body);
  } catch {
    console.log("Failed to parse webhook body");
    return;
  }

  switch (event) {
    case "push":
      console.log(`Push to ${(payload as Record<string, string>).ref}`);
      break;
    case "pull_request":
      console.log(
        `PR action: ${(payload as Record<string, string>).action}`,
      );
      break;
    default:
      console.log(`Unhandled event: ${event}`);
  }
};

export { pipeline };
