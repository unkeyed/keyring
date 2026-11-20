// Runs before the app's CSS paints, so a dark theme never flashes light.
// A file rather than an inline script, because the CSP forbids inline scripts.
(function () {
  var t;
  try {
    t = localStorage.getItem("keyring-theme");
  } catch (e) {}
  var dark = t === "dark" || (t !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  if (dark) document.documentElement.classList.add("dark");
})();
