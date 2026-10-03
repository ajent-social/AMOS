(() => {
  const box = document.getElementById("payment-state");
  if (!box || box.dataset.confirmed === "true" || !box.dataset.intent) return;
  const max = Number(box.dataset.polls) || 0;
  let count = 0;
  let busy = false;

  async function check() {
    if (busy || count >= max) return;
    busy = true;
    count += 1;
    try {
      const response = await fetch(
        "/billing/status/" + encodeURIComponent(box.dataset.intent),
        {
          credentials: "same-origin",
          headers: { Accept: "application/json" },
        },
      );
      if (response.ok) {
        const state = await response.json();
        if (state.confirmed === true) {
          location.replace("/billing");
          return;
        }
        if (state.state === "unavailable") {
          document.getElementById("pending-message").textContent =
            "Payment status is temporarily unavailable. Use Check again to retry.";
        }
      }
    } catch {
      // A failed read never changes the paid state; bounded polling/manual retry remain available.
    } finally {
      busy = false;
    }
    if (count < max) setTimeout(check, 1500);
    else {
      const message = document.getElementById("pending-message");
      if (message) {
        message.textContent =
          "Payment is still being verified. Use Check again to refresh status.";
      }
    }
  }

  document.getElementById("retry")?.addEventListener("click", () => {
    count = 0;
    check();
  });
  setTimeout(check, 500);
})();
