// Landscape background: its colours follow the time of day, pastel in light
// mode and deep in dark mode. The sun crosses the sky by day and the moon by
// night, at fixed hours: the server doesn't know where the visitor is.
const landscape = document.querySelector(".landscape");
if (landscape) {
  const dark = matchMedia("(prefers-color-scheme: dark)");
  const colors = ["sky-top", "sky-bottom", "sun", "hill-1", "hill-2", "hill-3", "hill-4"];
  // From hour 0, each [hour, glow, stars, colors]; between them it blends.
  const palettes = {
    light: [
      [0, 0.4, 0.7, "#bfc4ea #dcdaf3 #ffffff #b3b4e0 #a3a3d4 #9393c8 #8484bb"],
      [5, 0.4, 0.5, "#c8ccef #ecd9e8 #ffffff #c0bce0 #b1abd5 #a29bca #938bbe"],
      [6.5, 0.6, 0, "#d2daf5 #ffd5c2 #ffb08c #dcc6da #cdb5cf #bea4c4 #af93b8"],
      [9, 0.6, 0, "#bde2f4 #eaf5fa #fff0c4 #b6dacd #a0cdbe #8bc0ae #76b29f"],
      [13, 0.55, 0, "#a7dbf2 #e6f5fa #fff6d8 #b0dfcd #99d2be #83c5ae #6db79e"],
      [17, 0.65, 0, "#c2dbf1 #ffe4bf #ffcb85 #d4d6b5 #c4c8a3 #b4b991 #a3aa7f"],
      [19.5, 0.65, 0.1, "#d0caee #ffc6b8 #ff9c86 #d9bcd4 #c9aac7 #b898ba #a786ac"],
      [21, 0.45, 0.5, "#c1c3ea #ded3ee #ffffff #b6b2de #a7a2d3 #9892c7 #8982bb"],
    ],
    dark: [
      [0, 0.15, 0.8, "#05070f #121a33 #c9d3e8 #0f152a #0b1022 #080c19 #05080f"],
      [5, 0.15, 0.5, "#070a17 #272244 #c9d3e8 #181a35 #12142b #0d0f21 #080a15"],
      [6.5, 0.3, 0, "#121836 #5e3548 #e89a74 #2a2342 #1f1a34 #151327 #0c0c18"],
      [9, 0.25, 0, "#0e2034 #27465a #e6d4a4 #1c3341 #152935 #0f1f29 #09141b"],
      [13, 0.25, 0, "#0b2132 #1d4757 #efe2bf #173943 #112d36 #0c2129 #07161c"],
      [17, 0.3, 0, "#131f38 #554335 #eeb466 #2a2b37 #20212c #161721 #0e0f16"],
      [19.5, 0.3, 0.1, "#191736 #643043 #ec8666 #291e37 #1f172b #151020 #0c0a14"],
      [21, 0.15, 0.5, "#0a0c1f #221e40 #c9d3e8 #141631 #0f1127 #0a0c1c #060811"],
    ],
  };
  const rise = 6.5;
  const set = 19.5;
  const sun = landscape.querySelector(".sun");
  const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const blend = (a, b, t) =>
    "#" + rgb(a).map((x, i) => Math.round(x + (rgb(b)[i] - x) * t).toString(16).padStart(2, "0")).join("");

  // Colours go on <html>: its background shows under the iOS bars.
  function paint() {
    const now = new Date();
    const h = now.getHours() + now.getMinutes() / 60;
    const frames = palettes[dark.matches ? "dark" : "light"];
    const i = frames.findLastIndex((f) => f[0] <= h);
    const a = frames[i];
    const b = frames[i + 1] ?? [24, ...frames[0].slice(1)];
    const t = (h - a[0]) / (b[0] - a[0]);
    const from = a[3].split(" ");
    const to = b[3].split(" ");
    const style = document.documentElement.style;
    colors.forEach((name, j) => style.setProperty("--" + name, blend(from[j], to[j], t)));
    style.setProperty("--glow", a[1] + (b[1] - a[1]) * t);
    style.setProperty("--stars", a[2] + (b[2] - a[2]) * t);
    // How far across the sky, 0 to 1: the sun from rise to set, then the moon.
    const p = h >= rise && h < set ? (h - rise) / (set - rise) : ((h - set + 24) % 24) / (24 - set + rise);
    sun.setAttribute("transform", `translate(${350 + p * 900} ${720 - Math.sin(p * Math.PI) * 230})`);
  }
  paint();
  setInterval(paint, 60_000);
  dark.addEventListener("change", paint);
}
