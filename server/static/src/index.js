// Import asciinema-player
import * as AsciinemaPlayer from "asciinema-player";

// Import graph module
import { initGraph } from "./graph.js";

// Import results module
import { initResults } from "./results.js";

// Make AsciinemaPlayer available globally
window.AsciinemaPlayer = AsciinemaPlayer;

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
});
