// Pipelines module - handles trigger buttons and status polling

export function initPipelines() {
  // Set up trigger buttons
  const triggerButtons = document.querySelectorAll(".trigger-btn");
  triggerButtons.forEach((btn) => {
    btn.addEventListener("click", handleTriggerClick);
  });
}

async function handleTriggerClick(event) {
  const button = event.currentTarget;
  const pipelineId = button.dataset.pipelineId;
  const pipelineName = button.dataset.pipelineName || "Pipeline";

  // Disable button and show loading state
  button.disabled = true;
  const btnText = button.querySelector(".btn-text");
  const btnLoading = button.querySelector(".btn-loading");
  if (btnText) btnText.classList.add("hidden");
  if (btnLoading) btnLoading.classList.remove("hidden");

  try {
    // Trigger the pipeline
    const response = await fetch(`/api/pipelines/${pipelineId}/trigger`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || `HTTP ${response.status}`);
    }

    const result = await response.json();
    showToast(`${pipelineName} triggered successfully!`, "success");

    // Start polling for status updates
    pollRunStatus(result.run_id, pipelineName);

    // If we're on the pipeline detail page, add the new run to the table
    const runsTable = document.getElementById("runs-table");
    if (runsTable) {
      // Hide the "No runs yet" message and show the table
      const noRunsMessage = document.getElementById("no-runs-message");
      const runsTableContainer = document.getElementById(
        "runs-table-container"
      );
      if (noRunsMessage) {
        noRunsMessage.classList.add("hidden");
      }
      if (runsTableContainer) {
        runsTableContainer.classList.remove("hidden");
      }
      addRunToTable(runsTable, result);
    }
  } catch (error) {
    console.error("Failed to trigger pipeline:", error);
    showToast(`Failed to trigger: ${error.message}`, "error");
  } finally {
    // Re-enable button
    button.disabled = false;
    if (btnText) btnText.classList.remove("hidden");
    if (btnLoading) btnLoading.classList.add("hidden");
  }
}

function addRunToTable(table, run) {
  // Create a new row for the triggered run
  const row = document.createElement("tr");
  row.className = "hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors";
  row.dataset.runId = run.run_id;
  row.innerHTML = `
    <td class="px-6 py-4">
      <code class="text-sm text-gray-700 dark:text-gray-300">${run.run_id}</code>
    </td>
    <td class="px-6 py-4">
      <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200 status-badge">
        <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd" />
        </svg>
        queued
      </span>
    </td>
    <td class="px-6 py-4 text-sm text-gray-600 dark:text-gray-300">
      <span class="text-gray-400 dark:text-gray-500">—</span>
    </td>
    <td class="px-6 py-4 text-sm text-gray-600 dark:text-gray-300">
      <span class="text-gray-400 dark:text-gray-500">—</span>
    </td>
    <td class="px-6 py-4 text-right">
      <div class="flex items-center justify-end gap-2">
        <a href="/runs/${run.run_id}/tasks"
          class="px-3 py-1.5 bg-gray-200 hover:bg-gray-300 dark:bg-gray-700 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-200 rounded text-sm transition-colors"
          title="View task list">
          Tasks
        </a>
        <a href="/runs/${run.run_id}/graph"
          class="px-3 py-1.5 bg-blue-100 hover:bg-blue-200 dark:bg-blue-900 dark:hover:bg-blue-800 text-blue-700 dark:text-blue-200 rounded text-sm transition-colors"
          title="View dependency graph">
          Graph
        </a>
      </div>
    </td>
  `;

  // Insert at the top of the table
  table.insertBefore(row, table.firstChild);
}

function pollRunStatus(runId, pipelineName) {
  const pollInterval = 2000; // 2 seconds
  const maxPolls = 300; // 10 minutes max
  let pollCount = 0;

  const poll = async () => {
    try {
      const response = await fetch(`/api/runs/${runId}/status`);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const run = await response.json();

      // Update the status badge in the table if present
      updateRunRow(runId, run);

      // Check if the run is complete
      if (run.status === "success") {
        showToast(`${pipelineName} completed successfully!`, "success");
        return;
      } else if (run.status === "failed") {
        showToast(
          `${pipelineName} failed: ${run.error_message || "Unknown error"}`,
          "error"
        );
        return;
      }

      // Continue polling if still running
      pollCount++;
      if (
        pollCount < maxPolls &&
        (run.status === "queued" || run.status === "running")
      ) {
        setTimeout(poll, pollInterval);
      }
    } catch (error) {
      console.error("Failed to poll run status:", error);
      // Continue polling on error
      pollCount++;
      if (pollCount < maxPolls) {
        setTimeout(poll, pollInterval);
      }
    }
  };

  // Start polling after a short delay
  setTimeout(poll, 1000);
}

function updateRunRow(runId, run) {
  const row = document.querySelector(`tr[data-run-id="${runId}"]`);
  if (!row) return;

  const statusBadge = row.querySelector(".status-badge");
  if (!statusBadge) return;

  // Update the status badge based on run status
  let badgeHtml = "";
  switch (run.status) {
    case "success":
      badgeHtml = `
        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 status-badge">
          <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
          </svg>
          Success
        </span>`;
      break;
    case "failed":
      badgeHtml = `
        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200 status-badge">
          <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
          </svg>
          Failed
        </span>`;
      break;
    case "running":
      badgeHtml = `
        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200 status-badge">
          <svg class="animate-spin w-3 h-3" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          Running
        </span>`;
      break;
    default:
      badgeHtml = `
        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200 status-badge">
          <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd" />
          </svg>
          ${run.status}
        </span>`;
  }

  statusBadge.outerHTML = badgeHtml;
}

function showToast(message, type = "info") {
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
