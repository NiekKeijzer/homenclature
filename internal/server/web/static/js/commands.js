// Commands, listed while the search starts with ">" (see commands.go). They
// can be grouped: ">" alone lists the top level, where a command opens each
// group. One that needs a key asks for it in the pill (the prompt), then
// posts it; the answer is the command list, updated. search.js calls setup.
const main = document.querySelector("main");
const search = document.querySelector(".search input");
const prompt = document.querySelector(".prompt");
let group = ""; // the open command group; "" for the top level
let update; // the search's, see setup

export const enabled = !!document.getElementById("commands");
export const prompting = () => main.classList.contains("prompting");
export const keyField = prompt?.elements.key;

// open lists the commands in a group, or "" for the top level.
function open(inGroup) {
  group = inGroup;
  search.value = ">";
  update(false);
  search.focus();
}

// show lists the commands for q, the words after ">". No words list the open
// group, or the top level. Words search the titles of all commands (or the
// open group), every word in any order: "> key immich".
export function show(q) {
  main.classList.toggle("in-group", group !== "");
  const words = q.split(/\s+/).filter(Boolean);
  for (const c of document.querySelectorAll(".command")) {
    const listed = group ? c.dataset.group === group : words.length > 0 || !c.dataset.group;
    const text = c.querySelector(".title").textContent.toLowerCase();
    c.hidden = !listed || !words.every((w) => text.includes(w));
  }
  const opener = group && document.querySelector(`.command[data-opens="${group}"] .title`);
  document.querySelector("#commands h2").textContent = opener ? opener.textContent : "Commands";
}

// close leaves the commands: the next ">" lists the top level.
export function close() {
  group = "";
  main.classList.remove("in-group");
}

// back goes from a group to the top level, for Escape. False if no group is
// open.
export function back() {
  if (!group) return false;
  open("");
  return true;
}

// cancel ends the prompt, if it's open.
export function cancel() {
  if (prompting()) endPrompt();
}

const note = (text, error) => {
  const el = document.querySelector(".command-note");
  el.textContent = text;
  el.classList.toggle("error", !!error);
};

function run(command) {
  const { opens, key, action, name, help } = command.dataset;
  if (opens) return open(opens);
  if (action !== "save") {
    // Remove a key, or move all keys: nothing to ask.
    const body = new URLSearchParams({ action });
    if (key) body.set("app", key);
    return send(body, command.dataset.group);
  }
  prompt.elements.app.value = key;
  prompt.dataset.name = name;
  prompt.dataset.group = command.dataset.group;
  prompt.querySelector(".prompt-label").textContent = `${name} key`;
  keyField.placeholder = "Paste your key";
  keyField.setAttribute("aria-label", `Your ${name} key`);
  note(`Make one in ${name}: ${help}. Enter saves it, Escape cancels.`);
  main.classList.add("prompting");
  keyField.focus();
}

function endPrompt() {
  main.classList.remove("prompting");
  keyField.value = "";
  keyField.readOnly = false;
  note("");
}

// send posts a key command. Afterwards its group is open, to show the
// change.
async function send(body, inGroup) {
  const resp = await fetch("/commands/keys", { method: "POST", body }).catch(() => null);
  if (resp?.status === 401) return location.assign("/auth/login");
  const text = (await resp?.text())?.trim();
  if (!resp?.ok) {
    keyField.readOnly = false;
    keyField.select();
    return note(text || "That didn't work. Try again.", true);
  }
  cancel();
  document.getElementById("commands").outerHTML = text;
  open(inGroup ?? "");
}

// setup starts the commands. update is the search's: it shows what matches
// the search.
export function setup(searchUpdate) {
  update = searchUpdate;

  prompt?.addEventListener("submit", (e) => {
    e.preventDefault();
    if (keyField.readOnly) return; // already checking
    keyField.readOnly = true;
    note(`Checking the key with ${prompt.dataset.name}…`);
    send(new URLSearchParams(new FormData(prompt)), prompt.dataset.group);
  });
  prompt?.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      endPrompt();
      search.focus();
    }
  });

  // The hint under the empty search starts command mode. It keeps the focus
  // in the search, so the hint doesn't go away under the pointer.
  const hint = document.querySelector(".command-hint");
  hint?.addEventListener("pointerdown", (e) => e.preventDefault());
  hint?.addEventListener("click", () => open(""));

  document.addEventListener("click", (e) => {
    const command = e.target.closest("button.command");
    if (command) run(command);
  });
}
