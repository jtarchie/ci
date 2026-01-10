// Pipelines module - handles toast notifications for htmx trigger buttons

export function initPipelines() {
  // Listen for htmx events to show toast notifications
  document.body.addEventListener("htmx:afterRequest", handleHtmxRequest);
  document.body.addEventListener("htmx:responseError", handleHtmxError);
}

function handleHtmxRequest(event) {
  const trigger = event.detail.elt;
  if (!trigger?.classList.contains("trigger-btn") || !event.detail.successful) {
    return;
  }

  const pipelineName = trigger.dataset.pipelineName || "Pipeline";
  showToast(`${pipelineName} triggered successfully!`, "success");
}

function handleHtmxError(event) {
  const trigger = event.detail.elt;
  if (!trigger?.classList.contains("trigger-btn")) return;

  const pipelineName = trigger.dataset.pipelineName || "Pipeline";
  showToast(`Failed to trigger ${pipelineName}`, "error");
}

export function showToast(message, type = "info") {
  const container = document.getElementById("toast-container");
  if (!container) return;

  const toast = document.createElement("div");
  const bgColor = type === "success"
    ? "bg-green-600"
    : type === "error"
    ? "bg-red-600"
    : "bg-blue-600";

  toast.className =
    `${bgColor} text-white px-4 py-3 rounded-lg shadow-lg flex items-center gap-2 transform transition-all duration-300 translate-x-full`;
  toast.innerHTML = `
    <span>${message}</span>
    <button class="ml-2 hover:opacity-75" onclick="this.parentElement.remove()">
      <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
      </svg>
    </button>
  `;

  container.appendChild(toast);

  // Animate in
  requestAnimationFrame(() => {
    toast.classList.remove("translate-x-full");
  });

  // Auto-remove after 5 seconds
  setTimeout(() => {
    toast.classList.add("translate-x-full");
    setTimeout(() => toast.remove(), 300);
  }, 5000);
}
