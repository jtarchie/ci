/**
 * Polling management for HTMx dynamic updates
 * Manages polling intervals based on run status
 */

export function initPolling() {
  // Start polling management for runs section
  setupRunsPolling();

  // Listen for htmx afterSettle to check status changes
  document.body.addEventListener("htmx:afterSettle", function (event) {
    if (event.detail.target && event.detail.target.id === "runs-section") {
      checkAndTriggerPolling();
    }
  });

  // Initial check
  checkAndTriggerPolling();
}

function setupRunsPolling() {
  const runsSection = document.getElementById("runs-section");
  if (!runsSection) return;

  // Check immediately if we need to poll
  checkAndTriggerPolling();
}

function checkAndTriggerPolling() {
  const runsSection = document.getElementById("runs-section");
  if (!runsSection) return;

  // Check if there are any running or queued runs
  const hasActiveRuns = document.querySelector(
    '[data-run-status="running"], [data-run-status="queued"]',
  );

  if (hasActiveRuns) {
    // Trigger a custom event to start polling
    document.body.dispatchEvent(new Event("poll-runs"));
  }
}

// Expose utility to manually trigger polling
window.triggerRunsPolling = function () {
  document.body.dispatchEvent(new Event("poll-runs"));
};
