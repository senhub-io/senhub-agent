#!/usr/bin/env python3
"""Produit la liste des composants tiers reellement lies dans le binaire.

Ecrite depuis le graphe de dependances du build, pas depuis go.mod : go.mod
porte aussi ce que seuls les tests utilisent, et une annexe contractuelle ne
doit nommer que ce qui part chez le client.
"""
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
MAIN = "senhub-agent.go"

# L'ordre compte : le premier motif qui matche gagne, et les licences BSD se
# distinguent par une clause que la precedente n'a pas.
LICENCES = [
    ("Apache-2.0", r"Apache License\s+Version 2\.0"),
    # Certains modules ecrivent le titre en toutes lettres et en minuscules
    # ("Mozilla Public License, version 2.0"), d'autres en majuscules sans
    # virgule : un seul motif souple couvre les deux.
    ("MPL-2.0", r"Mozilla Public License,?\s+version 2\.0"),
    # zlib : reconnaissable a sa clause de non-garantie et a l'obligation de
    # ne pas se faire passer pour l'auteur.
    ("Zlib", r"this software is provided 'as-is'.{0,400}?misrepresented"),
    ("BSD-3-Clause", r"Neither the name of .{0,120}? may be used to endorse"),
    ("BSD-2-Clause", r"Redistributions in binary form must reproduce"),
    ("ISC", r"\bISC License\b"),
    ("MIT", r"Permission is hereby granted, free of charge"),
]
NAMES = ("LICENSE", "LICENCE", "LICENSE.md", "LICENSE.txt", "LICENSE-MIT",
         "COPYING", "COPYING.md", "NOTICE")


def linked_modules():
    """Les modules dont au moins un paquet entre dans le binaire de l'agent."""
    out = subprocess.run(
        ["go", "list", "-deps", "-f",
         "{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}",
         "./cmd/agent"],
        cwd=ROOT, capture_output=True, text=True, check=True).stdout
    seen = {}
    for line in out.splitlines():
        if not line.strip():
            continue
        parts = (line.split("\t") + ["", ""])[:3]
        path, version, directory = parts
        if not path or path == MAIN:
            continue
        seen[path] = (version, directory)
    return dict(sorted(seen.items()))


def identify(directory):
    """Rend le nom de la licence lue dans le module, ou None."""
    if not directory:
        return None
    base = pathlib.Path(directory)
    if not base.is_dir():
        return None
    # Linux distingue la casse, macOS non : "license.md" doit etre trouve
    # sur les deux, sinon le test passe ici et echoue en CI.
    present = {f.name.lower(): f for f in base.iterdir() if f.is_file()}
    for name in NAMES:
        f = present.get(name.lower())
        if f is None:
            continue
        text = f.read_text(errors="replace")[:8000]
        for label, pattern in LICENCES:
            if re.search(pattern, text, re.S | re.I):
                return label
        return "a verifier"
    return None


def main():
    mods = linked_modules()
    rows = []
    unknown = []
    for path, (version, directory) in mods.items():
        lic = identify(directory)
        if lic is None or lic == "a verifier":
            unknown.append(path)
            lic = lic or "non trouvee"
        rows.append((path, version or "(remplace)", lic))

    counts = {}
    for _, _, lic in rows:
        counts[lic] = counts.get(lic, 0) + 1

    lines = [
        "# Third-party components of SenHub Agent",
        "",
        "The components below are linked into the distributed binary. They are",
        "published by third parties under their own licences, which apply to",
        "those components alone.",
        "",
        "This list is generated from the build's dependency graph rather than",
        "from the module file, so it describes what actually ships rather than",
        "what is declared: a module used only by the tests does not appear here.",
        "",
        "Regenerate with `make third-party-notices`.",
        "",
        "## Summary",
        "",
        "| Licence | Components |",
        "|---|---|",
    ]
    for lic, n in sorted(counts.items(), key=lambda kv: (-kv[1], kv[0])):
        lines.append("| %s | %d |" % (lic, n))
    lines += ["", "Total: %d components." % len(rows), "", "## Detail", "",
              "| Component | Version | Licence |", "|---|---|---|"]
    for path, version, lic in rows:
        lines.append("| `%s` | %s | %s |" % (path, version, lic))
    lines.append("")

    dest = ROOT / "THIRD-PARTY-NOTICES.md"
    dest.write_text("\n".join(lines))
    print("%s: %d components, %d distinct licences"
          % (dest.relative_to(ROOT), len(rows), len(counts)))
    if unknown:
        print("licence not identified for: " + ", ".join(unknown), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
