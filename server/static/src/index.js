// Import htmx
import "htmx.org";

// Import asciinema-player
import * as AsciinemaPlayer from "asciinema-player";

// Import highlight.js core and languages
import hljs from "highlight.js/lib/core";
import yaml from "highlight.js/lib/languages/yaml";
import typescript from "highlight.js/lib/languages/typescript";
import javascript from "highlight.js/lib/languages/javascript";

// Register languages
hljs.registerLanguage("yaml", yaml);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("javascript", javascript);

// Import graph module
import { initGraph } from "./graph.js";

// Import results module
import { initResults } from "./results.js";

// Import pipelines module
import { initPipelines, showToast } from "./pipelines.js";

// Import polling management
import { initPolling } from "./polling.js";

// Make AsciinemaPlayer available globally
window.AsciinemaPlayer = AsciinemaPlayer;

// Initialize syntax highlighting
function initSyntaxHighlighting() {
  document.querySelectorAll("pre code").forEach((block) => {
    hljs.highlightElement(block);
  });
}

// Export CI namespace for htmx hx-on attributes
window.CI = { showToast, initSyntaxHighlighting };

// Add global HTMx error handling
document.body.addEventListener("htmx:responseError", function (event) {
  console.error("HTMx error:", event.detail);
  const statusCode = event.detail.xhr?.status;
  const message = statusCode === 404
    ? "Resource not found"
    : statusCode === 500
    ? "Server error occurred"
    : statusCode === 0
    ? "Network error - please check your connection"
    : "An error occurred";
  showToast(message, "error");
});

// Add loading state management
document.body.addEventListener("htmx:beforeRequest", function (event) {
  if (event.detail.target) {
    event.detail.target.setAttribute("aria-busy", "true");
  }
});

document.body.addEventListener("htmx:afterSettle", function (event) {
  if (event.detail.target) {
    event.detail.target.removeAttribute("aria-busy");
  }
});

// Initialize when DOM is ready
document.addEventListener("DOMContentLoaded", function () {
  // Initialize graph if we're on the graph page
  const graphDataElement = document.getElementById("graph-data");
  if (graphDataElement) {
    try {
      const graphData = JSON.parse(graphDataElement.textContent);
      const currentPath = graphDataElement.dataset.path || "/";
      initGraph(graphData, currentPath);
    } catch (e) {
      console.error("Failed to initialize graph:", e);
    }
  }

  // Initialize results page if we're on the results page
  const tasksContainer = document.getElementById("tasks-container");
  if (tasksContainer) {
    try {
      initResults();
    } catch (e) {
      console.error("Failed to initialize results:", e);
    }
  }

  // Initialize pipelines page if we're on a pipelines page
  const pipelinesTable = document.getElementById("pipelines-table");
  const triggerBtn = document.getElementById("trigger-btn");

  // Initialize syntax highlighting
  initSyntaxHighlighting();
  if (pipelinesTable || triggerBtn) {
    try {
      initPipelines();
    } catch (e) {
      console.error("Failed to initialize pipelines:", e);
    }
  }

  // Initialize polling management
  try {
    initPolling();
  } catch (e) {
    console.error("Failed to initialize polling:", e);
  }
});
