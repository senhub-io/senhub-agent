/* Interactive tag filter for release-note pages.
   Looks for a `.rn-filter` placeholder; if present, builds one button per
   distinct tag found in the page's `ul.rn` entries, and filters entries
   (and hides emptied section headings) on click. Uses Material's document$
   so it re-initialises on instant navigation. */
(function () {
  function init() {
    var host = document.querySelector(".rn-filter");
    if (!host || host.dataset.ready) return;

    var items = Array.prototype.slice.call(
      document.querySelectorAll(".md-content ul.rn > li")
    );
    if (!items.length) return;

    // Collect distinct tags (preserve first-seen order), keep their chip class.
    var order = [];
    var classByLabel = {};
    items.forEach(function (li) {
      li.querySelectorAll(".tag").forEach(function (t) {
        var label = t.textContent.trim();
        if (!(label in classByLabel)) {
          classByLabel[label] = t.className; // e.g. "tag t-fixed"
          order.push(label);
        }
      });
    });

    host.innerHTML = "";
    var lbl = document.createElement("span");
    lbl.className = "rn-filter-label";
    lbl.textContent = "Filter:";
    host.appendChild(lbl);

    function makeBtn(label, cls) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = cls + " rn-fbtn";
      b.dataset.tag = label;
      b.textContent = label;
      return b;
    }

    var all = makeBtn("All", "tag rn-all");
    all.classList.add("active");
    host.appendChild(all);
    order.forEach(function (label) {
      host.appendChild(makeBtn(label, classByLabel[label]));
    });

    var active = null;

    function apply() {
      items.forEach(function (li) {
        var labels = Array.prototype.map.call(
          li.querySelectorAll(".tag"),
          function (t) { return t.textContent.trim(); }
        );
        li.style.display = !active || labels.indexOf(active) !== -1 ? "" : "none";
      });
      document.querySelectorAll(".md-content ul.rn").forEach(function (ul) {
        var anyVisible = Array.prototype.some.call(ul.children, function (li) {
          return li.style.display !== "none";
        });
        ul.style.display = anyVisible ? "" : "none";
        var h = ul.previousElementSibling;
        while (h && !/^H[1-6]$/.test(h.tagName)) h = h.previousElementSibling;
        if (h && h.tagName === "H2") h.style.display = anyVisible ? "" : "none";
      });
    }

    host.addEventListener("click", function (e) {
      var btn = e.target.closest(".rn-fbtn");
      if (!btn) return;
      host.querySelectorAll(".rn-fbtn").forEach(function (b) {
        b.classList.remove("active");
      });
      btn.classList.add("active");
      active = btn.dataset.tag === "All" ? null : btn.dataset.tag;
      apply();
    });

    host.dataset.ready = "1";
  }

  if (typeof document$ !== "undefined" && document$.subscribe) {
    document$.subscribe(init);
  } else {
    document.addEventListener("DOMContentLoaded", init);
  }
})();
