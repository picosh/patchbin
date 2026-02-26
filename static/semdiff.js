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
