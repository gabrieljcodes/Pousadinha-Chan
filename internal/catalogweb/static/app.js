const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const paths = {
  grid: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  star: '<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2-5.6-3-5.6 3 1.1-6.2L3 9.6l6.2-.9Z"/>',
  image:
    '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8" cy="8" r="1.5"/><path d="m3 17 5-5 4 4 4-7 5 8"/>',
  archive:
    '<rect x="3" y="4" width="18" height="4" rx="1"/><path d="M5 8v12h14V8m-10 5h6"/>',
  layers: '<path d="m12 3 10 5-10 5L2 8Zm-9 10 9 5 9-5M3 18l9 5 9-5"/>',
  logout: '<path d="M9 4H4v16h5m5-13 5 5-5 5m-5-5h10"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
  list: '<path d="M9 5h12M9 12h12M9 19h12M3 5h1M3 12h1M3 19h1"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  heart:
    '<path d="M20.5 5.5c-2-2-5-1.5-8.5 2-3.5-3.5-6.5-4-8.5-2S2 11 5 14l7 7 7-7c3-3 3.5-6.5 1.5-8.5Z"/>',
  back: '<path d="m10 5-7 7 7 7M3 12h18"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  upload: '<path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5"/>',
  refresh:
    '<path d="M20 7v5h-5M4 17v-5h5m-4-5a8 8 0 0 1 14-2l1 2M4 17l1 2a8 8 0 0 0 14-2"/>',
  external: '<path d="M14 3h7v7m0-7L10 14m0-10H4v16h16v-6"/>',
};
const icon = (name) =>
  `<svg class="icon" aria-hidden="true" viewBox="0 0 24 24">${paths[name] || paths.image}</svg>`;
const escape = (v) =>
  String(v ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const number = (v) => Number(v || 0).toLocaleString("en-US");
const short = (v) =>
  Number(v || 0) >= 1000
    ? new Intl.NumberFormat("en", {
        notation: "compact",
        maximumFractionDigits: 1,
      }).format(v)
    : number(v);
const imageURL = (id) => `/catalog/api/assets/${id}/file`;
const safeURL = (value) => {
  try {
    const u = new URL(value);
    return ["https:", "http:"].includes(u.protocol) ? escape(u.href) : "";
  } catch {
    return "";
  }
};
let current = null,
  view = "catalog",
  page = 1,
  dirty = false,
  listController,
  detailSequence = 0,
  toastTimer,
  searchTimer;
let catalogScroll = 0;
let listLayout = false,
  lastCatalogHash = "#catalog",
  uploadBusy = false;
$$("[data-icon]").forEach((el) => (el.innerHTML = icon(el.dataset.icon)));

async function api(path, options = {}) {
  const response = await fetch(`/catalog/api/${path}`, {
    ...options,
    headers: {
      "X-Catalog-Request": "1",
      ...(options.body instanceof FormData
        ? {}
        : { "Content-Type": "application/json" }),
      ...options.headers,
    },
  });
  const result = await response.json().catch(() => ({}));
  if (response.status === 401 && path !== "login") showLogin();
  if (!response.ok)
    throw new Error(
      result.error || `Request failed (${response.status}). Please retry.`,
    );
  return result;
}
const write = (path, method, body) =>
  api(path, { method, body: JSON.stringify(body) });
function toast(message) {
  clearTimeout(toastTimer);
  $("#toast").textContent = message;
  $("#toast").hidden = false;
  toastTimer = setTimeout(() => ($("#toast").hidden = true), 5000);
}
function showLogin() {
  $("#login").hidden = false;
  $("#app").hidden = true;
}
async function start() {
  try {
    await api("session");
    $("#login").hidden = true;
    $("#app").hidden = false;
    await route();
    refreshStats();
  } catch {
    showLogin();
  }
}
$("#login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const b = $("button", e.target);
  b.disabled = true;
  $("#login-error").textContent = "";
  try {
    await write("login", "POST", { password: $("#password").value });
    $("#password").value = "";
    await start();
  } catch (err) {
    $("#login-error").textContent = err.message;
  } finally {
    b.disabled = false;
  }
});
$$("#logout, #logout-mobile").forEach((button) =>
  button.addEventListener("click", async () => {
    if (!(await mayLeave())) return;
    try {
      await write("logout", "POST", {});
      showLogin();
    } catch (e) {
      toast(e.message);
    }
  }),
);
async function refreshStats() {
  try {
    const s = await api("stats");
    $("#count-all").textContent = short(s.characters);
    $("#count-favorites").textContent = short(s.favorites);
    $("#count-pending").textContent = short(s.pending);
    $("#library-summary").textContent =
      `${number(s.characters)} characters · ${number(s.photos)} photos`;
  } catch (e) {
    toast(e.message);
  }
}
function routeParts() {
  return location.hash.slice(1).split("?");
}
async function route() {
  detailSequence++;
  const [path, query = ""] = routeParts();
  if (path.startsWith("character/")) {
    const id = Number(path.split("/")[1]);
    if (!Number.isSafeInteger(id) || id < 1) {
      location.hash = "catalog";
      return;
    }
    if (!current) catalogScroll = window.scrollY;
    await loadDetail(id);
    $("#main").focus({ preventScroll: true });
    window.scrollTo({ top: 0, behavior: "instant" });
    return;
  }
  const returningFromDetail = !!current;
  lastCatalogHash = location.hash || "#catalog";
  current = null;
  view = ["favorites", "review", "archived"].includes(path) ? path : "catalog";
  page = Math.max(1, Number(new URLSearchParams(query).get("page")) || 1);
  $("#detail-view").hidden = true;
  $("#catalog-view").hidden = false;
  $("#breadcrumb").textContent = {
    catalog: "Characters",
    favorites: "Favorites",
    review: "Photo review",
    archived: "Archive",
  }[view];
  $("#page-title").textContent = {
    catalog: "Character catalog",
    favorites: "Your favorites",
    review: "Photo review",
    archived: "Character archive",
  }[view];
  $("#page-description").textContent = {
    catalog: "Discover, organize, and make the collection your own.",
    favorites: "A handpicked collection. Independent of AniList likes.",
    review: "Review new photos before they appear in the bot.",
    archived: "Out of the rolls. Safely kept, ready to restore.",
  }[view];
  $$("[data-nav]").forEach((a) =>
    a.classList.toggle("active", a.dataset.nav === view),
  );
  const params = new URLSearchParams(query);
  for (const el of $("#filters").elements) {
    if (el.name) el.value = params.get(el.name) || "";
  }
  $("#sort").value = params.get("sort") || "likes";
  if (view === "review") $("#filters").elements.status.value = "pending";
  await loadList();
  if (returningFromDetail) {
    $("#main").focus({ preventScroll: true });
    window.scrollTo({ top: catalogScroll, behavior: "instant" });
  }
}
let lastRouteHash = location.hash;
window.addEventListener("hashchange", async () => {
  if (!(await mayLeave())) {
    history.replaceState(null, "", lastRouteHash || "#catalog");
    return;
  }
  lastRouteHash = location.hash;
  route();
});
window.addEventListener("beforeunload", (e) => {
  if (dirty || uploadBusy) {
    e.preventDefault();
    e.returnValue = "";
  }
});
document.addEventListener("click", async (e) => {
  const link = e.target.closest('a[href^="#"]');
  if (link && dirty) {
    e.preventDefault();
    if (await mayLeave()) location.hash = link.getAttribute("href");
  }
});
function updateFilters() {
  const p = new URLSearchParams(new FormData($("#filters")));
  p.set("sort", $("#sort").value);
  for (const [k, v] of [...p]) if (!v) p.delete(k);
  location.hash = `${view}?${p}`;
}
$("#filters").addEventListener("submit", (e) => {
  e.preventDefault();
  clearTimeout(searchTimer);
  updateFilters();
});
$("#search").addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(updateFilters, 300);
});
$$("select", $("#filters")).forEach((s) =>
  s.addEventListener("change", updateFilters),
);
$("#sort").addEventListener("change", updateFilters);
$("#reset-filters").addEventListener("click", () => {
  location.hash = view;
});
$("#layout-toggle").addEventListener("click", () => {
  listLayout = !listLayout;
  $("#characters").classList.toggle("list-layout", listLayout);
  $("#layout-toggle").innerHTML = icon(listLayout ? "grid" : "list");
  $("#layout-toggle").setAttribute(
    "aria-label",
    `Switch to ${listLayout ? "grid" : "list"} view`,
  );
});
async function loadList() {
  listController?.abort();
  listController = new AbortController();
  const controller = listController;
  const params = new URLSearchParams(new FormData($("#filters")));
  params.set("page", page);
  params.set("sort", $("#sort").value);
  params.set("view", view);
  $("#characters").setAttribute("aria-busy", "true");
  $("#characters").classList.add("loading");
  $("#catalog-error").hidden = true;
  try {
    const data = await api(`characters?${params}`, {
      signal: controller.signal,
    });
    $("#result-count").textContent =
      `${number(data.total)} character${data.total === 1 ? "" : "s"}${view === "favorites" ? " in favorites" : ""}`;
    $("#characters").innerHTML =
      data.items.map(card).join("") ||
      `<div class="empty-state">${icon(view === "favorites" ? "star" : "search")}<h2>No characters here yet.</h2><p>${view === "favorites" ? "Star a character to start your editorial collection." : "Try another search or add a character to the catalog."}</p><button data-empty-reset>Clear filters</button></div>`;
    const pages = Math.max(1, Math.ceil(data.total / data.page_size));
    $("#pagination").innerHTML =
      `<button data-page="${page - 1}" ${page <= 1 ? "disabled" : ""}>${icon("back")} Previous</button><span>Page ${number(page)} of ${number(pages)}</span><button data-page="${page + 1}" ${page >= pages ? "disabled" : ""}>Next <span aria-hidden="true">→</span></button>`;
  } catch (e) {
    if (e.name !== "AbortError") {
      $("#catalog-error").textContent = e.message;
      $("#catalog-error").hidden = false;
    }
  } finally {
    if (controller === listController) {
      $("#characters").classList.remove("loading");
      $("#characters").setAttribute("aria-busy", "false");
    }
  }
}
function card(c) {
  return `<article class="character-card"><a class="portrait-link" href="#character/${c.id}" aria-label="Open ${escape(c.name)}">${c.cover_id ? `<img loading="lazy" src="${imageURL(c.cover_id)}" alt="${escape(c.name)}" width="420" height="600">` : `<span class="missing-art">${icon("image")}No portrait yet</span>`}${!c.enabled ? `<span class="card-status">${c.archived_at ? "Archived" : "Not in rolls"}</span>` : ""}</a><a class="character-name" href="#character/${c.id}">${escape(c.name)}</a><p class="character-work" title="${escape(c.work)}">${escape(c.work)}</p><div class="card-meta">${icon("heart")}<span title="AniList likes">${short(c.favourites)}</span><span class="photo-count">${icon("image")}${number(c.asset_count)}</span></div><button class="card-favorite ${c.editorial_favorite ? "selected" : ""}" data-favorite="${c.id}" data-value="${!c.editorial_favorite}" aria-label="${c.editorial_favorite ? "Unfavorite" : "Favorite"} ${escape(c.name)}" aria-pressed="${c.editorial_favorite}">${icon("star")}</button></article>`;
}
$("#characters").addEventListener("click", async (e) => {
  if (e.target.closest("[data-empty-reset]")) {
    location.hash = "catalog";
    return;
  }
  const b = e.target.closest("[data-favorite]");
  if (!b) return;
  b.disabled = true;
  try {
    await write(`characters/${b.dataset.favorite}`, "PATCH", {
      action: "favorite",
      value: b.dataset.value === "true",
    });
    await loadList();
    refreshStats();
  } catch (err) {
    toast(err.message);
    b.disabled = false;
  }
});
$("#pagination").addEventListener("click", (e) => {
  const b = e.target.closest("[data-page]");
  if (!b) return;
  const [path, query] = routeParts();
  const p = new URLSearchParams(query);
  p.set("page", b.dataset.page);
  location.hash = `${path}?${p}`;
  window.scrollTo({ top: 0, behavior: "instant" });
});

async function confirmAction(title, message, label = "Archive") {
  const d = $("#confirm-dialog");
  $("#confirm-title").textContent = title;
  $("#confirm-message").textContent = message;
  $('[value="confirm"]', d).textContent = label;
  d.returnValue = "cancel";
  d.showModal();
  return new Promise((resolve) =>
    d.addEventListener("close", () => resolve(d.returnValue === "confirm"), {
      once: true,
    }),
  );
}
async function mayLeave() {
  if (uploadBusy) {
    toast("Wait for photo processing to finish before leaving.");
    return false;
  }
  if (!dirty) return true;
  const okay = await confirmAction(
    "Discard unsaved changes?",
    "Your metadata edits have not been saved.",
    "Discard changes",
  );
  if (okay) dirty = false;
  return okay;
}
async function loadDetail(id) {
  const sequence = ++detailSequence;
  $("#catalog-view").hidden = true;
  $("#detail-view").hidden = false;
  $("#detail-view").innerHTML = "<p>Loading character…</p>";
  try {
    const c = await api(`characters/${id}`);
    if (sequence !== detailSequence) return;
    current = c;
    dirty = false;
    $("#breadcrumb").textContent = c.name;
    renderDetail();
  } catch (e) {
    $("#detail-view").innerHTML =
      `<a class="back-link" href="${escape(lastCatalogHash)}">Back to catalog</a><p class="error">${escape(e.message)}</p>`;
  }
}
function renderDetail() {
  const c = current;
  $("#detail-view").innerHTML =
    `<a class="back-link" href="${escape(lastCatalogHash)}">${icon("back")}Back to catalog</a><div class="detail-heading"><div><h1>${escape(c.name)}</h1><p>${escape(c.work)}${c.native_name ? " · " + escape(c.native_name) : ""}</p><div class="badges"><span class="badge ${c.enabled ? "good" : ""}">${c.archived_at ? "Archived" : c.enabled ? "Available in rolls" : "Not in rolls"}</span><span class="badge">${icon("heart")}${number(c.favourites)} likes</span><span class="badge">${number(c.base_value)} base coins</span><span class="badge">${number(c.owners)} server claims</span><span class="badge">ID ${c.id}</span></div></div><div class="heading-actions"><button data-character-action="favorite" data-value="${!c.editorial_favorite}">${icon("star")}${c.editorial_favorite ? "Favorited" : "Favorite"}</button>${c.archived_at ? `<button data-character-action="archive" data-value="false">Restore</button>` : `<button data-character-action="enable" data-value="${!c.enabled}">${c.enabled ? "Disable rolls" : "Enable rolls"}</button><button data-character-action="archive" data-value="true" class="icon-button" aria-label="Archive character" title="Archive character">${icon("archive")}</button>`}</div></div>
 <div class="detail-workspace"><section class="photo-section"><div class="section-heading"><h2>Photo collection <span class="optional">${c.assets.filter((a) => !a.archived_at).length}</span></h2><select id="photo-filter" aria-label="Filter photos"><option value="all">All photos</option><option value="favorites">Favorites</option><option value="pending">Pending review</option><option value="archived">Archived photos</option></select></div>
 ${c.archived_at ? '<div class="notice">Restore this character to add photos. Claims and history are preserved.</div>' : `<div class="dropzone" id="dropzone">${icon("upload")}<p>Drop photos here to add to the collection.</p><button id="browse-photos">Choose photos</button><input id="photo-files" type="file" accept="image/png,image/jpeg,image/gif" multiple hidden><small>PNG, JPEG or GIF · Up to 12 MiB each · Rendered at 420 × 600</small><label class="upload-label"><input id="approve-uploads" type="checkbox" checked>Approve these photos for the bot</label><p class="upload-progress" id="upload-progress" role="status"></p></div>`}<div class="photo-grid" id="photos"></div></section>
 <aside class="editor"><h2>Character details</h2><form id="edit-form"><label>Name<input name="name" required maxlength="250" value="${escape(c.name)}"></label><label>Native name<input name="native_name" maxlength="250" value="${escape(c.native_name)}"></label><div class="field-row"><label>Gender<select name="gender">${["", "Female", "Male", "Non-binary", "Other"].map((g) => `<option value="${g}" ${c.gender === g ? "selected" : ""}>${g || "Unspecified"}</option>`).join("")}</select></label><label>AniList likes<input name="favourites" type="number" min="0" max="2147483647" required value="${c.favourites}"></label></div><label>Aliases <span class="optional">one per line</span><textarea name="aliases" rows="3">${escape((c.aliases || []).join("\n"))}</textarea></label><label>Description<textarea name="description" rows="6" maxlength="20000">${escape(c.description)}</textarea></label><details><summary>Link another work</summary><label>Work title<input name="work" maxlength="250"></label><label>Universe<select name="kind"><option value="anime">Anime</option><option value="manga">Manga</option><option value="game">Game</option><option value="other">Other</option></select></label><label>Genres <span class="optional">comma-separated</span><input name="genres"></label><label>Studios <span class="optional">comma-separated</span><input name="studios"></label></details><p class="form-error error" role="alert"></p><button type="submit" class="primary">Save changes</button></form><hr><h3>Linked works</h3>${c.works.map((work) => `<div class="work-item"><strong>${escape(work.title)}</strong><small>${escape(work.kind)}${work.genres?.length ? " · " + escape(work.genres.join(", ")) : ""}${work.studios?.length ? "<br>" + escape(work.studios.join(", ")) : ""}</small></div>`).join("") || '<p class="fine">No works linked yet.</p>'}<hr><h3>Sources</h3>${c.sources.map((src) => (safeURL(src.source_url) ? `<p class="fine"><a href="${safeURL(src.source_url)}" target="_blank" rel="noopener noreferrer">${escape(src.provider)} · ${escape(src.external_id)} ↗</a></p>` : "")).join("") || '<p class="fine">Manually curated character.</p>'}<p class="fine">Last edited ${escape(new Date(c.updated_at).toLocaleString())}</p></aside></div>`;
  renderPhotos();
  $("#photo-filter").addEventListener("change", renderPhotos);
  $("#edit-form").addEventListener("input", () => (dirty = true));
  $("#edit-form").addEventListener("submit", saveDetails);
  $("#browse-photos")?.addEventListener("click", () =>
    $("#photo-files").click(),
  );
  $("#photo-files")?.addEventListener("change", (e) =>
    uploadFiles(e.target.files),
  );
  const drop = $("#dropzone");
  if (drop) {
    drop.addEventListener("dragover", (e) => {
      e.preventDefault();
      drop.classList.add("drag-over");
    });
    drop.addEventListener("dragleave", () =>
      drop.classList.remove("drag-over"),
    );
    drop.addEventListener("drop", (e) => {
      e.preventDefault();
      drop.classList.remove("drag-over");
      uploadFiles(e.dataTransfer.files);
    });
  }
}
function renderPhotos() {
  const filter = $("#photo-filter").value;
  const assets = current.assets.filter((a) =>
    filter === "archived"
      ? a.archived_at
      : !a.archived_at &&
        (filter !== "favorites" || a.editorial_favorite) &&
        (filter !== "pending" || a.status === "pending"),
  );
  $("#photos").innerHTML =
    assets
      .map(
        (a) =>
          `<article class="photo-tile ${a.archived_at ? "archived" : ""}" data-asset="${a.id}"><div class="photo-image"><img loading="lazy" src="${imageURL(a.id)}" alt="${escape(current.name)} — photo ${a.id}" width="420" height="600">${a.is_primary ? '<span class="badge good">Main portrait</span>' : a.media_type === "image/gif" ? '<span class="badge">GIF</span>' : ""}<button class="card-favorite ${a.editorial_favorite ? "selected" : ""}" data-asset-action="favorite" data-value="${!a.editorial_favorite}" aria-label="${a.editorial_favorite ? "Unfavorite" : "Favorite"} photo ${a.id}" aria-pressed="${a.editorial_favorite}">${icon("star")}</button></div><div class="photo-tools">${a.archived_at ? '<button data-asset-action="archive" data-value="false">Restore photo</button>' : `<button data-asset-action="primary" ${a.is_primary ? "disabled" : ""}>${a.is_primary ? icon("check") : "Set portrait"}</button><button class="icon-button" data-replace="${a.id}" title="Replace photo" aria-label="Replace photo ${a.id}">${icon("refresh")}</button><button class="icon-button" data-asset-action="archive" data-value="true" title="Archive photo" aria-label="Archive photo ${a.id}">${icon("archive")}</button>`}</div>${!a.archived_at ? `<select data-review aria-label="Review status of photo ${a.id}">${["approved", "pending", "rejected"].map((status) => `<option value="${status}" ${status === a.status ? "selected" : ""}>${{ approved: "Approved", pending: "Pending review", rejected: "Rejected" }[status]}</option>`).join("")}</select>` : ""}<input class="credit" data-credit aria-label="Attribution for photo ${a.id}" placeholder="Add artist credit…" value="${escape(a.attribution)}" maxlength="2000"><small class="source">${safeURL(a.source_url) ? `<a href="${safeURL(a.source_url)}" target="_blank" rel="noopener noreferrer">${escape(a.provider)} ↗</a>` : escape(a.provider)} · #${a.id}</small></article>`,
      )
      .join("") ||
    '<div class="empty-state"><h2>Room for another portrait.</h2><p>Add photos or change the photo filter.</p></div>';
}
async function saveDetails(e) {
  e.preventDefault();
  const f = e.target,
    b = $("[type=submit]", f);
  b.disabled = true;
  $(".form-error", f).textContent = "";
  const data = new FormData(f);
  const input = {
    name: data.get("name"),
    native_name: data.get("native_name"),
    gender: data.get("gender"),
    favourites: Number(data.get("favourites")),
    aliases: data
      .get("aliases")
      .split("\n")
      .map((x) => x.trim())
      .filter(Boolean),
    description: data.get("description"),
    updated_at: current.updated_at,
  };
  if (data.get("work").trim())
    input.new_work = {
      title: data.get("work").trim(),
      kind: data.get("kind"),
      genres: data
        .get("genres")
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
      studios: data
        .get("studios")
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
    };
  try {
    await write(`characters/${current.id}`, "PUT", input);
    dirty = false;
    await loadDetail(current.id);
    toast("Character details saved.");
    refreshStats();
  } catch (err) {
    $(".form-error", f).textContent = err.message;
  } finally {
    b.disabled = false;
  }
}
$("#detail-view").addEventListener("click", async (e) => {
  const action = e.target.closest("[data-character-action]");
  if (action) {
    if (!(await mayLeave())) return;
    const name = action.dataset.characterAction,
      value = action.dataset.value === "true";
    if (
      name === "archive" &&
      value &&
      !(await confirmAction(
        "Archive this character?",
        "This removes the character from rolls. Photos, claims and ownership history stay intact. You can restore it later.",
      ))
    )
      return;
    action.disabled = true;
    try {
      await write(`characters/${current.id}`, "PATCH", { action: name, value });
      await loadDetail(current.id);
      refreshStats();
      toast("Character updated.");
    } catch (err) {
      toast(err.message);
      action.disabled = false;
    }
    return;
  }
  const replace = e.target.closest("[data-replace]");
  if (replace) {
    if (!(await mayLeave())) return;
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/png,image/jpeg,image/gif";
    input.addEventListener("change", () =>
      uploadFiles(input.files, replace.dataset.replace),
    );
    input.click();
    return;
  }
  const b = e.target.closest("[data-asset-action]");
  if (!b) return;
  if (!(await mayLeave())) return;
  const id = b.closest("[data-asset]").dataset.asset;
  const name = b.dataset.assetAction,
    value = b.dataset.value === "true";
  if (
    name === "archive" &&
    value &&
    !(await confirmAction(
      "Archive this photo?",
      "It will be hidden from the bot. The original file remains available here for restoration.",
    ))
  )
    return;
  b.disabled = true;
  try {
    await write(`assets/${id}`, "PATCH", { action: name, value });
    const filter = $("#photo-filter").value;
    await loadDetail(current.id);
    $("#photo-filter").value = filter;
    renderPhotos();
    refreshStats();
    toast("Photo updated.");
  } catch (err) {
    toast(err.message);
    b.disabled = false;
  }
});
$("#detail-view").addEventListener("change", async (e) => {
  const input = e.target;
  if (!input.matches("[data-review],[data-credit]")) return;
  const id = input.closest("[data-asset]").dataset.asset;
  const body = input.matches("[data-review]")
    ? { action: "review", status: input.value }
    : { action: "attribution", attribution: input.value };
  input.disabled = true;
  try {
    await write(`assets/${id}`, "PATCH", body);
    const c = await api(`characters/${current.id}`);
    current = c;
    renderPhotos();
    refreshStats();
    toast("Photo updated.");
  } catch (err) {
    toast(err.message);
  } finally {
    input.disabled = false;
  }
});
async function uploadFiles(fileList, replaceID = "") {
  if (uploadBusy) {
    toast("Wait for the current upload to finish.");
    return;
  }
  if (!fileList.length || !(await mayLeave())) return;
  if (fileList.length > 20) {
    toast("Upload up to 20 photos at a time.");
    return;
  }
  const cid = current.id;
  uploadBusy = true;
  const approved = replaceID ? true : $("#approve-uploads").checked;
  let done = 0;
  const errors = [];
  try {
    for (const file of fileList) {
      if (file.size > 12 * 1024 * 1024) {
        errors.push(`${file.name}: exceeds 12 MiB`);
        continue;
      }
      const progress = $("#upload-progress");
      if (progress)
        progress.textContent = `Processing ${done + 1} of ${fileList.length}: ${file.name}`;
      const form = new FormData();
      form.append("file", file);
      form.append("approve", approved);
      if (replaceID) form.append("replace_id", replaceID);
      try {
        await api(`characters/${cid}/assets`, { method: "POST", body: form });
        done++;
      } catch (e) {
        errors.push(`${file.name}: ${e.message}`);
      }
    }
    if (current?.id === cid) {
      await loadDetail(cid);
      if (errors.length) {
        const p = $("#upload-progress");
        if (p) p.textContent = errors.join(" · ");
      }
    }
    refreshStats();
    toast(
      `${done} photo${done === 1 ? "" : "s"} added.${errors.length ? " Some uploads need attention." : ""}`,
    );
  } finally {
    uploadBusy = false;
  }
}
for (const [button, dialog] of [
  ["create-open", "create-dialog"],
  ["import-open", "import-dialog"],
])
  $("#" + button).addEventListener("click", () => {
    $(".form-error", $("#" + dialog)).textContent = "";
    $("#" + dialog).showModal();
  });
$$("[data-close]").forEach((b) =>
  b.addEventListener("click", () => b.closest("dialog").close()),
);
$("#create-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = e.target,
    b = $("[type=submit]", f),
    data = new FormData(f);
  b.disabled = true;
  $(".form-error", f).textContent = "";
  const input = {
    name: data.get("name"),
    native_name: data.get("native_name"),
    gender: data.get("gender"),
    aliases: [],
    favourites: 0,
    description: "",
  };
  if (data.get("work").trim())
    input.new_work = {
      title: data.get("work"),
      kind: data.get("kind"),
      genres: [],
      studios: [],
    };
  try {
    const c = await write("characters", "POST", input);
    $("#create-dialog").close();
    f.reset();
    location.hash = `character/${c.id}`;
    refreshStats();
    toast("Character created. Add a portrait to get started.");
  } catch (err) {
    $(".form-error", f).textContent = err.message;
  } finally {
    b.disabled = false;
  }
});
$("#import-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = e.target,
    b = $("[type=submit]", f);
  const source = new FormData(f).get("source").trim();
  const match = source.match(
    /^(?:https:\/\/anilist\.co\/character\/)?(\d+)(?:\/[^\s]*)?$/,
  );
  if (!match) {
    $(".form-error", f).textContent =
      "Enter a numeric ID or an AniList character URL.";
    return;
  }
  b.disabled = true;
  b.textContent = "Importing…";
  $(".form-error", f).textContent = "";
  try {
    const c = await write("import", "POST", { id: Number(match[1]) });
    $("#import-dialog").close();
    f.reset();
    location.hash = `character/${c.id}`;
    refreshStats();
    toast("Character imported.");
  } catch (err) {
    $(".form-error", f).textContent = err.message;
  } finally {
    b.disabled = false;
    b.textContent = "Import character";
  }
});
document.addEventListener(
  "error",
  (e) => {
    if (e.target.tagName === "IMG" && e.target.src.includes("/api/assets/")) {
      const note = document.createElement("span");
      note.className = "missing-art image-error";
      note.textContent = "Photo file unavailable";
      e.target.replaceWith(note);
    }
  },
  true,
);
start();
