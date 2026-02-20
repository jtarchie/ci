// Minimal webhook handler - echoes back request info
const pipeline = () => {
  const req = http.request();

  if (req) {
    // Respond immediately with a JSON summary of the incoming request
    http.respond({
      status: 200,
      body: JSON.stringify({
        received: true,
        method: req.method,
        query: req.query,
      }),
      headers: { "Content-Type": "application/json" },
    });
  }

  // Pipeline continues after response is sent
  console.log("Webhook processing complete");
};

export { pipeline };
