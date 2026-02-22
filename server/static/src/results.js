/**
 * Results page functionality
 * - Search/filter tasks
 * - Expand/collapse all
 * - Keyboard navigation
 * - Stats display
 * - Help panel
 * - Live polling with state preservation
 */

// Track expanded task state across htmx swaps
const expandedTasks = new Set();

export function initResults() {
  const searchInput = document.getElementById("task-search");
  const expandAllBtn = document.getElementById("expand-all");
  const collapseAllBtn = document.getElementById("collapse-all");
  const helpToggle = document.getElementById("help-toggle");
  const helpPanel = document.getElementById("help-panel");

  function getTasksContainer() {
    return document.getElementById("tasks-container");
  }

  // Bail if required elements don't exist
  if (!getTasksContainer()) return;

  // Restore expanded state from previous swap
  restoreExpandedState(getTasksContainer());

  // Listen for htmx before swap to save state
  document.body.addEventListener("htmx:beforeSwap", handleBeforeSwap);

  // Listen for htmx after swap to restore state
  document.body.addEventListener("htmx:afterSwap", handleAfterSwap);

  // Help panel toggle
  if (helpToggle && helpPanel) {
    helpToggle.addEventListener("click", function () {
      const isHidden = helpPanel.classList.contains("hidden");
      helpPanel.classList.toggle("hidden");
      helpToggle.setAttribute("aria-expanded", isHidden);

      // Focus management - focus first element in panel when opening
      if (isHidden) {
        helpPanel.focus();
      }
    });

    // Close help panel when clicking outside
    document.addEventListener("click", function (e) {
      if (!helpToggle.contains(e.target) && !helpPanel.contains(e.target)) {
        helpPanel.classList.add("hidden");
        helpToggle.setAttribute("aria-expanded", "false");
      }
    });

    // Close help panel with Escape key
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && !helpPanel.classList.contains("hidden")) {
        helpPanel.classList.add("hidden");
        helpToggle.setAttribute("aria-expanded", "false");
        helpToggle.focus(); // Return focus to toggle button
      }
    });
  }

  // Get all task items
  function getAllTasks() {
    const container = getTasksContainer();
    return container ? container.querySelectorAll(".task-item") : [];
  }

  // Update stats
  function updateStats() {
    const tasks = getAllTasks();
    let success = 0,
      failure = 0,
      pending = 0;
    tasks.forEach((task) => {
      if (
        task.classList.contains("bg-green-100") ||
        task.classList.contains("dark:bg-green-900/30")
      ) {
        success++;
      } else if (
        task.classList.contains("bg-red-100") ||
        task.classList.contains("dark:bg-red-900/30")
      ) {
        failure++;
      } else {
        pending++;
      }
    });
    const successEl = document.getElementById("stat-success");
    const failureEl = document.getElementById("stat-failure");
    const pendingEl = document.getElementById("stat-pending");
    if (successEl) successEl.textContent = success;
    if (failureEl) failureEl.textContent = failure;
    if (pendingEl) pendingEl.textContent = pending;
  }
  updateStats();

  // Search/filter functionality
  if (searchInput) {
    searchInput.addEventListener("input", function (e) {
      const query = e.target.value.toLowerCase();
      const tasks = getAllTasks();
      const container = getTasksContainer();
      const groups = container
        ? container.querySelectorAll(".group-container")
        : [];

      tasks.forEach((task) => {
        const name =
          task.querySelector(".font-medium")?.textContent.toLowerCase() || "";
        const matches = query === "" || name.includes(query);
        task.style.display = matches ? "" : "none";
      });

      // Show groups that have visible children
      groups.forEach((group) => {
        const visibleTasks = group.querySelectorAll(
          '.task-item:not([style*="display: none"])',
        );
        const visibleGroups = group.querySelectorAll(
          '.group-container:not([style*="display: none"])',
        );
        group.style.display =
          visibleTasks.length > 0 || visibleGroups.length > 0 || query === ""
            ? ""
            : "none";
      });
    });
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
    // Skip if typing in search
    if (searchInput && e.target === searchInput) {
      if (e.key === "Escape") {
        searchInput.value = "";
        searchInput.dispatchEvent(new Event("input"));
        searchInput.blur();
      }
      return;
    }

    const tasks = Array.from(getAllTasks()).filter(
      (t) => t.style.display !== "none",
    );
    if (tasks.length === 0) return;

    switch (e.key) {
      case "j": // Next task
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
      case "k": // Previous task
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
        if (e.target.tagName !== "INPUT" && expandAllBtn) {
          expandAllBtn.click();
        }
        break;
      case "c":
        if (e.target.tagName !== "INPUT" && collapseAllBtn) {
          collapseAllBtn.click();
        }
        break;
      case "f":
        if (e.target.tagName !== "INPUT") {
          // Jump to first failure
          const firstFailure = tasksContainer.querySelector(
            ".task-item.bg-red-100, .task-item.dark\\:bg-red-900\\/30",
          );
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

// Handle htmx beforeSwap - save expanded state
function handleBeforeSwap(event) {
  const target = event.detail.target;
  if (!target || target.id !== "pipeline") return;

  const responseText = event.detail.xhr?.responseText || "";
  if (
    responseText &&
    normalizeHtml(responseText) === normalizeHtml(target.innerHTML)
  ) {
    event.detail.shouldSwap = false;
    return;
  }

  const container = target.querySelector("#tasks-container");
  if (!container) return;

  // Save expanded task state
  saveExpandedState(container);
}

// Handle htmx afterSwap - restore expanded state and reinitialize
function handleAfterSwap(event) {
  const target = event.detail.target;
  if (!target || target.id !== "pipeline") return;

  const container = target.querySelector("#tasks-container");
  if (!container) return;

  // Restore expanded state
  restoreExpandedState(container);

  // Update stats
  updateStatsForContainer(container);
}

function normalizeHtml(value) {
  return value.replace(/\s+/g, " ").trim();
}

// Save which tasks are expanded
function saveExpandedState(container) {
  expandedTasks.clear();
  const tasks = container.querySelectorAll(".task-item[open]");
  tasks.forEach((task) => {
    const taskId = task.dataset.taskId;
    if (taskId) {
      expandedTasks.add(taskId);
    }
  });
}

// Restore expanded state to tasks
function restoreExpandedState(container) {
  if (expandedTasks.size === 0) return;

  const tasks = container.querySelectorAll(".task-item");
  tasks.forEach((task) => {
    const taskId = task.dataset.taskId;
    if (taskId && expandedTasks.has(taskId)) {
      task.setAttribute("open", "");
    }
  });
}

// Update stats for a given container
function updateStatsForContainer(container) {
  const tasks = container.querySelectorAll(".task-item");
  let success = 0,
    failure = 0,
    pending = 0;
  tasks.forEach((task) => {
    if (
      task.classList.contains("bg-green-100") ||
      task.classList.contains("dark:bg-green-900/30")
    ) {
      success++;
    } else if (
      task.classList.contains("bg-red-100") ||
      task.classList.contains("dark:bg-red-900/30")
    ) {
      failure++;
    } else {
      pending++;
    }
  });
  const successEl = document.getElementById("stat-success");
  const failureEl = document.getElementById("stat-failure");
  const pendingEl = document.getElementById("stat-pending");
  if (successEl) successEl.textContent = success;
  if (failureEl) failureEl.textContent = failure;
  if (pendingEl) pendingEl.textContent = pending;
}
