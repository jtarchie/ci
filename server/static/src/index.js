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
});
