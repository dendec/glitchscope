"use strict";

const tabs = [...document.querySelectorAll('[role="tab"]')];
const panels = [...document.querySelectorAll(".platform-panel")];
const tablist = document.querySelector('[role="tablist"]');

function selectPlatform(id, focus = false) {
  if (!panels.some((panel) => panel.id === id)) return;
  for (const tab of tabs) {
    const selected = tab.getAttribute("aria-controls") === id;
    tab.setAttribute("aria-selected", String(selected));
    tab.tabIndex = selected ? 0 : -1;
    if (selected && focus) tab.focus();
  }
  for (const panel of panels) panel.hidden = panel.id !== id;
}

if (tablist) {
  tablist.hidden = false;
  for (const panel of panels) {
    panel.setAttribute("role", "tabpanel");
    panel.setAttribute("aria-labelledby", `tab-${panel.id}`);
    panel.tabIndex = 0;
  }
  selectPlatform(location.hash.slice(1) || "portmaster");
  if (!panels.some((panel) => panel.id === location.hash.slice(1))) selectPlatform("portmaster");
  for (const [index, tab] of tabs.entries()) {
    tab.addEventListener("click", () => {
      const id = tab.getAttribute("aria-controls");
      selectPlatform(id);
      history.replaceState(null, "", `#${id}`);
    });
    tab.addEventListener("keydown", (event) => {
      const target = { ArrowRight: (index + 1) % tabs.length, ArrowLeft: (index + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
      if (target === undefined) return;
      event.preventDefault();
      const id = tabs[target].getAttribute("aria-controls");
      selectPlatform(id, true);
      history.replaceState(null, "", `#${id}`);
    });
  }
  // Reveal a hidden platform before the browser follows an in-page link.
  for (const link of document.querySelectorAll('.platform-strip a')) {
    link.addEventListener("click", () => selectPlatform(link.hash.slice(1)));
  }
  window.addEventListener("hashchange", () => {
    const id = location.hash.slice(1);
    selectPlatform(id);
    if (panels.some((panel) => panel.id === id)) document.getElementById(id).scrollIntoView();
  });
}

for (const link of document.querySelectorAll(".languages a")) {
  link.addEventListener("click", () => {
    try { localStorage.setItem("glitchscope-language", link.lang); } catch {}
    const selected = document.querySelector('[role="tab"][aria-selected="true"]');
    link.hash = location.hash && panels.some((panel) => `#${panel.id}` === location.hash)
      ? selected.getAttribute("aria-controls") : location.hash;
  });
}

if (navigator.clipboard && window.isSecureContext) {
  for (const button of document.querySelectorAll(".copy")) {
    button.hidden = false;
    button.addEventListener("click", async () => {
      const status = document.getElementById("copy-status");
      try {
        await navigator.clipboard.writeText(button.closest(".command").querySelector("code").textContent);
        button.textContent = document.body.dataset.copied;
        status.textContent = document.body.dataset.copied;
      } catch {
        status.textContent = document.body.dataset.copyFailed;
        button.closest(".command").querySelector("pre").focus();
      }
      window.setTimeout(() => { button.textContent = document.body.dataset.copy; status.textContent = ""; }, 2500);
    });
  }
}
