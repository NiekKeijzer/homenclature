// Clock, date and greeting in the visitor's local time; the server doesn't
// know their timezone.
const clock = document.querySelector(".clock");
const greeting = document.querySelector(".greeting");
const name = greeting.dataset.name;

function tick() {
  const now = new Date();
  const h = now.getHours();
  const part = h >= 5 && h < 12 ? "morning" : h >= 12 && h < 18 ? "afternoon" : "evening";
  const date = now.toLocaleDateString("en-GB", { weekday: "long", day: "numeric", month: "long" });
  clock.textContent = now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  greeting.textContent = `${date} · Good ${part}${name ? ", " + name : ""}`;
}
tick();
setInterval(tick, 1000);

// Search: filters links as you type. "/" focuses it, Enter opens the first
// match, Escape clears it.
const search = document.querySelector(".search input");
if (search) {
  const links = [...document.querySelectorAll(".link")];
  const sections = [...document.querySelectorAll(".panel")];
  const noMatches = document.querySelector(".no-matches");

  search.addEventListener("input", () => {
    const q = search.value.trim().toLowerCase();
    let first = null;
    for (const link of links) {
      const match = link.querySelector(".text").textContent.toLowerCase().includes(q);
      link.hidden = !match;
      link.classList.remove("active");
      if (match && !first) first = link;
    }
    for (const s of sections) s.hidden = !s.querySelector(".link:not([hidden])");
    if (q) first?.classList.add("active");
    noMatches.hidden = !q || first !== null;
  });

  search.addEventListener("keydown", (e) => {
    if (e.key === "Enter") document.querySelector(".link.active")?.click();
    if (e.key === "Escape") {
      search.value = "";
      search.dispatchEvent(new Event("input"));
      search.blur();
    }
  });

  document.addEventListener("keydown", (e) => {
    if (e.key === "/" && document.activeElement !== search) {
      e.preventDefault();
      search.focus();
    }
  });
}
