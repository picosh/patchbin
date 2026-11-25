document.addEventListener("click", function (event) {
  var link = event.target.closest("[data-open-details]");
  if (!link) {
    return;
  }
  var hash = link.getAttribute("href");
  if (!hash || hash.charAt(0) !== "#") {
    return;
  }
  var target = document.getElementById(hash.slice(1));
  if (!target) {
    return;
  }
  var details = target.closest("details");
  if (details && !details.open) {
    details.open = true;
  }
});

function getPatchFileDetails(container) {
  return container.querySelectorAll(":scope > details");
}

function syncToggleAllDiffsButton(button, container) {
  var detailsList = getPatchFileDetails(container);
  var anyOpen = Array.prototype.some.call(detailsList, function (d) {
    return d.open;
  });
  button.textContent = anyOpen ? "collapse all" : "expand all";
  button.setAttribute("aria-expanded", anyOpen ? "true" : "false");
}

document.addEventListener("click", function (event) {
  var button = event.target.closest("[data-toggle-all-diffs]");
  if (!button) {
    return;
  }
  var container = document.getElementById("patch-files");
  if (!container) {
    return;
  }
  var detailsList = getPatchFileDetails(container);
  var shouldOpen = !Array.prototype.some.call(detailsList, function (d) {
    return d.open;
  });
  Array.prototype.forEach.call(detailsList, function (d) {
    d.open = shouldOpen;
  });
  syncToggleAllDiffsButton(button, container);
});

document.addEventListener(
  "toggle",
  function (event) {
    var container = document.getElementById("patch-files");
    if (!container || event.target.parentElement !== container) {
      return;
    }
    var button = document.querySelector("[data-toggle-all-diffs]");
    if (button) {
      syncToggleAllDiffsButton(button, container);
    }
  },
  true
);
