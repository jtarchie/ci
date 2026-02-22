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
  const bgColor =
    type === "success"
      ? "bg-green-600"
      : type === "error"
        ? "bg-red-600"
        : "bg-blue-600";

  toast.className = `${bgColor} text-white px-4 py-3 rounded-lg shadow-lg flex items-center gap-2 transform transition-all duration-300 translate-x-full`;
  toast.setAttribute("role", "alert");
  toast.setAttribute("aria-live", "assertive");
  toast.setAttribute("aria-atomic", "true");

  // Create message span
  const messageSpan = document.createElement("span");
  messageSpan.textContent = message;
  toast.appendChild(messageSpan);

  // Create close button
  const closeBtn = document.createElement("button");
  closeBtn.className = "ml-2 hover:opacity-75";
  closeBtn.setAttribute("aria-label", "Close notification");
  closeBtn.addEventListener("click", () => toast.remove());

  // Create SVG for close button
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", "w-4 h-4");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");

  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("stroke-linecap", "round");
  path.setAttribute("stroke-linejoin", "round");
  path.setAttribute("stroke-width", "2");
  path.setAttribute("d", "M6 18L18 6M6 6l12 12");

  svg.appendChild(path);
  closeBtn.appendChild(svg);
  toast.appendChild(closeBtn);

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
