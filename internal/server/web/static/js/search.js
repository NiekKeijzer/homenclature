// Search: filters links as you type. If apps on the page can be searched,
// HTMX also loads results from inside them (2+ characters), with chips to
// filter by app. "/" focuses it, ↑/↓ select a result, Enter opens it, Tab
// moves to the next app chip and Escape clears. Starting with ">" lists
// commands instead (commands.js).
import { clock, greeting } from "./clock.js";
import * as commands from "./commands.js";

const search = document.querySelector(".search input");
if (search) {
  const main = document.querySelector("main");
  const links = [...document.querySelectorAll(".link")];
  const sections = [...document.querySelectorAll(".sections .panel")];
  const linkSections = document.querySelector(".sections");
  const appResults = document.getElementById("app-results");
  const noMatches = document.querySelector(".no-matches");
  const pill = document.querySelector(".search");
  const searchable = search.hasAttribute("hx-get");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)");
  let filter = ""; // data-app of the pressed chip; "" for all
  let active = null; // the selected result

  // Compact shrinks the clock to make room for results, while the search is
  // focused or has a query. The layout change is animated FLIP-style:
  // measure, change, then animate each element from where it was.
  function setCompact(on) {
    if (main.classList.contains("compact") === on) return;
    const els = [clock, greeting, ...[...main.children].slice(1)]; // all but the hero itself
    const before = els.map((el) => el.getBoundingClientRect());
    main.classList.toggle("compact", on);
    if (reduceMotion.matches) return;
    els.forEach((el, i) => {
      const a = before[i];
      const b = el.getBoundingClientRect();
      if (!a.height || !b.height) return; // hidden before or after
      const from = `translate(${a.left - b.left}px, ${a.top - b.top}px) scale(${a.height / b.height})`;
      el.animate([{ transform: from }, { transform: "none" }], {
        duration: 450,
        easing: "cubic-bezier(0.2, 0.8, 0.2, 1)",
      });
    });
  }

  const updateCompact = () => setCompact(pill.matches(":focus-within") || search.value.trim() !== "");
  pill.addEventListener("focusin", updateCompact);
  pill.addEventListener("focusout", (e) => {
    if (!pill.contains(e.relatedTarget)) setCompact(search.value.trim() !== "");
  });

  const chips = () => [...document.querySelectorAll(".chip")];
  // Visible results in page order: links first, then app results.
  const results = () => [...document.querySelectorAll(".link, .result")].filter((el) => el.offsetParent);

  function select(el, scroll) {
    active?.classList.remove("active");
    active = el ?? null;
    active?.classList.add("active");
    if (scroll) active?.scrollIntoView({ block: "nearest" });
  }

  function applyFilter() {
    if (!chips().some((c) => c.dataset.app === filter)) filter = "";
    for (const c of chips()) c.setAttribute("aria-pressed", c.dataset.app === filter);
    if (linkSections) linkSections.hidden = filter !== "";
    for (const s of appResults.querySelectorAll(".app")) s.hidden = filter !== "" && s.dataset.app !== filter;
  }

  // update shows what matches the query, or the commands that match after
  // ">". Without keepSelection, the first result is selected.
  function update(keepSelection) {
    // While asking for a key, the list and its selection stay as they are
    // (a late HTMX answer would clear the selection).
    if (commands.prompting()) return;
    const value = search.value.trim();
    const commanding = commands.enabled && value.startsWith(">");
    const q = (commanding ? value.slice(1).trim() : value).toLowerCase();
    main.classList.toggle("searching", value !== "");
    main.classList.toggle("commanding", commanding);
    updateCompact();
    if (commanding) {
      commands.show(q);
    } else {
      commands.close();
      for (const link of links) link.hidden = !link.querySelector(".text").textContent.toLowerCase().includes(q);
      for (const s of sections) s.hidden = !s.querySelector(".link:not([hidden])");
      applyFilter();
    }
    const all = results();
    if (!value) select(null);
    else if (!keepSelection || !all.includes(active)) select(all[0]);
    // While app results may still come, don't claim there are none.
    const waiting = searchable && !commanding && q.length >= 2 && !keepSelection;
    noMatches.hidden = !value || all.length > 0 || waiting;
  }

  commands.setup(update);

  function clear() {
    commands.cancel();
    if (searchable) {
      htmx.trigger(search, "htmx:abort");
      appResults.replaceChildren();
      document.getElementById("chips").replaceChildren();
    }
    search.value = "";
    search.dispatchEvent(new Event("input")); // also resets HTMX's "changed" check
  }

  search.addEventListener("input", () => update(false));
  document.addEventListener("htmx:afterSwap", () => update(true));

  search.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      const all = results();
      if (!all.length) return;
      e.preventDefault();
      let i = all.indexOf(active);
      if (e.key === "ArrowDown") i++;
      else i = (i < 0 ? all.length : i) - 1;
      select(all[(i + all.length) % all.length], true);
    } else if (e.key === "Enter") {
      active?.click();
    } else if (e.key === "Tab" && chips().length && !main.classList.contains("commanding")) {
      e.preventDefault();
      const keys = chips().map((c) => c.dataset.app);
      const i = keys.indexOf(filter) + (e.shiftKey ? -1 : 1);
      filter = keys[(i + keys.length) % keys.length];
      update(false);
    } else if (e.key === "Escape") {
      // In a command group, back to the top level. Not the browser's own
      // Escape, which empties the search.
      if (commands.back()) return e.preventDefault();
      clear();
      search.blur();
    }
  });

  document.addEventListener("click", (e) => {
    const chip = e.target.closest(".chip");
    if (chip) {
      filter = chip.dataset.app;
      update(false);
    }
  });

  // The pill around the input focuses it, like a label would.
  pill.addEventListener("click", (e) => {
    if (e.target.closest(".clear")) {
      search.focus(); // first, so the clock doesn't grow and shrink again
      clear();
    } else if (!e.target.closest("input")) {
      (commands.prompting() ? commands.keyField : search).focus();
    }
  });

  // Not while typing in a field: a key may contain "/".
  document.addEventListener("keydown", (e) => {
    if (e.key === "/" && !e.target.closest("input, textarea")) {
      e.preventDefault();
      search.focus();
    }
  });
}
