#!/usr/bin/env python3
"""Produit la liste des composants tiers reellement lies dans le binaire.

Ecrite depuis le graphe de dependances du build, pas depuis go.mod : go.mod
porte aussi ce que seuls les tests utilisent, et une annexe contractuelle ne
doit nommer que ce qui part chez le client.
"""
import os
import pathlib
import re
import subprocess
import sys

CORE_ROOT = pathlib.Path(__file__).resolve().parent.parent
CORE = "senhub-agent.go"
# The enterprise edition links the core and adds its own modules; its
# repository runs this script with these variables to list them all.
ROOT = pathlib.Path(os.environ.get("SENHUB_NOTICES_ROOT") or CORE_ROOT).resolve()
MAIN = os.environ.get("SENHUB_NOTICES_MAIN", CORE)
TITLE = os.environ.get("SENHUB_NOTICES_TITLE", "SenHub Agent")
REGEN = os.environ.get("SENHUB_NOTICES_REGEN", "make third-party-notices")

# L'ordre compte : le premier motif qui matche gagne, et les licences BSD se
# distinguent par une clause que la precedente n'a pas.
LICENCES = [
    # go-smbios ecrit le titre en markdown ("Apache License", un filet de "=",
    # puis "_Version 2.0_") ; yaml.v3 cite "Apache License, Version 2.0" dans
    # un fichier MIT, la virgule ne doit donc pas passer.
    ("Apache-2.0", r"Apache License[\s=_]+Version 2\.0"),
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
    """Les modules dont au moins un paquet entre dans un binaire distribue.

    Le graphe depend de GOOS (DPAPI, WMI et SMBIOS ne sont lies que sous
    Windows, sysconf que sous Linux) : on prend l'union des deux systemes
    livres, pour que le fichier ne depende pas de la machine qui le genere.
    """
    out = ""
    # Le compagnon eBPF (cmd/senhub-ebpf) n'existe que sous Linux.
    targets = (("linux", "./cmd/agent"), ("linux", "./cmd/senhub-ebpf"),
               ("windows", "./cmd/agent"))
    if ROOT != CORE_ROOT:
        targets = (("linux", "./cmd/agent"), ("windows", "./cmd/agent"))
    for goos, package in targets:
        env = dict(os.environ, GOOS=goos)
        out += subprocess.run(
            ["go", "list", "-deps", "-f",
             "{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}"
             "\t{{with .Module.Replace}}{{.Path}} {{.Version}}{{end}}{{end}}",
             package],
            cwd=ROOT, capture_output=True, text=True, check=True, env=env).stdout
    seen = {}
    for line in out.splitlines():
        if not line.strip():
            continue
        parts = (line.split("\t") + ["", "", ""])[:4]
        path, version, directory, replaced = parts
        if replaced.strip():
            # A fork stands in for the upstream module: name what ships.
            version = "replaced by %s" % replaced.strip()
        if not path or path in (MAIN, CORE):
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
        "# Third-party components of %s" % TITLE,
        "",
        "The components below are linked into the distributed binary. They are",
        "published by third parties under their own licences, which apply to",
        "those components alone.",
        "",
        "This list is generated from the build's dependency graph rather than",
        "from the module file, so it describes what actually ships rather than",
        "what is declared: a module used only by the tests does not appear here.",
        "",
        "Regenerate with `%s`." % REGEN,
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

    if ROOT != CORE_ROOT:
        if unknown:
            print("licence not identified for: " + ", ".join(unknown), file=sys.stderr)
            return 1
        return 0

    # The user guide publishes the same list as Annex 1 of the licence
    # agreement. It used to be copied by hand and fell behind the build;
    # it is written here from the same rows, only its preamble differs.
    doc = ROOT / "docs" / "user-guide" / "docs" / "license" / "third-party.md"
    annex = ("This page corresponds to Annex 1 of the [SenHub Agent license "
             "agreement](agreement-fr.md), and is refreshed with every published version.")
    doc.write_text("\n".join(annex if l.startswith("Regenerate with") else l for l in lines))
    print("%s: written from the same list" % doc.relative_to(ROOT))
    if unknown:
        print("licence not identified for: " + ", ".join(unknown), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
