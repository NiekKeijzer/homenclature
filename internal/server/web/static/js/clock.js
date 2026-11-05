// Clock, date and greeting in the visitor's local time; the server doesn't
// know their timezone.
export const clock = document.querySelector(".clock");
export const greeting = document.querySelector(".greeting");
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
