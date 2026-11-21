// Copies the deploy prompt shown on the landing page.
const source = document.getElementById("agent-prompt");
const toast = document.querySelector(".toast");
let hide;

async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // The Clipboard API needs a secure context and permission; fall back to a selection.
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

if (source && toast) {
  const prompt = source.textContent.trim();
  const buttons = document.querySelectorAll("[data-copy-prompt]");
  for (const button of buttons) {
    const label = button.querySelector(".copy-state");
    button.addEventListener("click", async () => {
      const ok = await copy(prompt);
      label.textContent = ok ? "Copied" : "Copy failed";
      toast.textContent = ok ? "Prompt copied. Paste it into your coding agent to deploy on Unkey." : "Could not copy. Select the prompt and copy it by hand.";
      toast.classList.add("show");
      clearTimeout(hide);
      hide = setTimeout(() => {
        toast.classList.remove("show");
        for (const b of buttons) b.querySelector(".copy-state").textContent = b.dataset.label;
      }, 2400);
    });
  }
}
