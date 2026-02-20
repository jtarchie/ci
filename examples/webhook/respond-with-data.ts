// Webhook that reads JSON body and responds with processed data
const pipeline = () => {
  const req = http.request();

  if (req) {
    let data: Record<string, unknown> = {};

    try {
      data = JSON.parse(req.body);
    } catch {
      http.respond({
        status: 400,
        body: JSON.stringify({ error: "invalid JSON body" }),
        headers: { "Content-Type": "application/json" },
      });
      return;
    }

    http.respond({
      status: 200,
      body: JSON.stringify({
        processed: true,
        keys: Object.keys(data),
        timestamp: new Date().toISOString(),
      }),
      headers: { "Content-Type": "application/json" },
    });
  }

  // Continue with pipeline work after responding
  console.log("Processing webhook payload in background");
};

export { pipeline };
