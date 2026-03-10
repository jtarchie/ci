/**
 * Results page functionality
 * - Expand/collapse all
 * - Keyboard navigation
 * - Help panel
 */

export function initResults() {
  const searchInput = document.getElementById("task-search");
  const expandAllBtn = document.getElementById("expand-all");
  const collapseAllBtn = document.getElementById("collapse-all");
  const helpToggle = document.getElementById("help-toggle");
  const helpPanel = document.getElementById("help-panel");

  function getTasksContainer() {
    return document.getElementById("tasks-container");
  }

  if (!getTasksContainer()) return;

  // Help panel toggle
  if (helpToggle && helpPanel) {
    helpToggle.addEventListener("click", function () {
      const isHidden = helpPanel.classList.contains("hidden");
      helpPanel.classList.toggle("hidden");
      helpToggle.setAttribute("aria-expanded", isHidden);
      if (isHidden) helpPanel.focus();
    });

    document.addEventListener("click", function (e) {
      if (!helpToggle.contains(e.target) && !helpPanel.contains(e.target)) {
        helpPanel.classList.add("hidden");
        helpToggle.setAttribute("aria-expanded", "false");
      }
    });

    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && !helpPanel.classList.contains("hidden")) {
        helpPanel.classList.add("hidden");
        helpToggle.setAttribute("aria-expanded", "false");
        helpToggle.focus();
      }
    });
  }

  function getAllTasks() {
    const container = getTasksContainer();
    return container
      ? Array.from(container.querySelectorAll(".task-item"))
      : [];
  }

  // Expand all
  if (expandAllBtn) {
    expandAllBtn.addEventListener("click", function () {
      getAllTasks().forEach((task) => task.setAttribute("open", ""));
    });
  }

  // Collapse all
  if (collapseAllBtn) {
    collapseAllBtn.addEventListener("click", function () {
      getAllTasks().forEach((task) => task.removeAttribute("open"));
    });
  }

  // Keyboard navigation
  let currentTaskIndex = -1;
  document.addEventListener("keydown", function (e) {
    if (searchInput && e.target === searchInput) {
      if (e.key === "Escape") {
        searchInput.value = "";
        searchInput.dispatchEvent(new Event("search"));
        searchInput.blur();
      }
      return;
    }

    const tasks = getAllTasks();
    if (tasks.length === 0) return;

    // Don't intercept shortcuts when modifier keys are held (e.g. Cmd+C to copy).
    if (e.metaKey || e.ctrlKey || e.altKey) return;

    switch (e.key) {
      case "j":
      case "ArrowDown":
        if (e.target.tagName !== "INPUT") {
          e.preventDefault();
          currentTaskIndex = Math.min(currentTaskIndex + 1, tasks.length - 1);
          tasks[currentTaskIndex].scrollIntoView({
            behavior: "smooth",
            block: "center",
          });
          tasks[currentTaskIndex].focus();
        }
        break;
      case "k":
      case "ArrowUp":
        if (e.target.tagName !== "INPUT") {
          e.preventDefault();
          currentTaskIndex = Math.max(currentTaskIndex - 1, 0);
          tasks[currentTaskIndex].scrollIntoView({
            behavior: "smooth",
            block: "center",
          });
          tasks[currentTaskIndex].focus();
        }
        break;
      case "Enter":
      case " ":
        if (e.target.classList.contains("task-item")) {
          e.preventDefault();
          e.target.toggleAttribute("open");
        }
        break;
      case "/":
        if (searchInput) {
          e.preventDefault();
          searchInput.focus();
        }
        break;
      case "e":
        if (e.target.tagName !== "INPUT" && expandAllBtn) expandAllBtn.click();
        break;
      case "c":
        if (e.target.tagName !== "INPUT" && collapseAllBtn) {
          collapseAllBtn.click();
        }
        break;
      case "f":
        if (e.target.tagName !== "INPUT") {
          const container = getTasksContainer();
          const firstFailure = container
            ? container.querySelector(
              ".task-item.bg-red-100, .task-item.dark\\:bg-red-900\\/30",
            )
            : null;
          if (firstFailure) {
            firstFailure.scrollIntoView({
              behavior: "smooth",
              block: "center",
            });
            firstFailure.focus();
            firstFailure.setAttribute("open", "");
          }
        }
        break;
    }
  });
}
