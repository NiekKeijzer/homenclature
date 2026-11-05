// Photo background: a new photo on load and every data-interval seconds,
// faded in over the last one. If one doesn't load, the last one stays.
const slideshow = document.querySelector(".slideshow");
if (slideshow) {
  const next = async () => {
    if (document.hidden && slideshow.querySelector("img")) return;
    const img = new Image();
    img.alt = "";
    img.src = "/background?t=" + Date.now();
    try {
      await img.decode();
    } catch {
      return;
    }
    const old = slideshow.querySelectorAll("img");
    slideshow.append(img);
    const duration = matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 1500;
    img.animate([{ opacity: 0 }, { opacity: 1 }], { duration, easing: "ease" }).finished.then(() => {
      for (const el of old) el.remove();
    });
  };
  next();
  setInterval(next, slideshow.dataset.interval * 1000);
}
