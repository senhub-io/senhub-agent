# Probe logos

One SVG per probe page, named after the page. They are served from here
rather than hot-linked, and that is the point of the directory.

## Why they live here

These marks used to be fetched from a public icon CDN at render time. Ten
of them stopped resolving without warning, which is what a brand
withdrawing its permission from an icon set looks like, and the pages
went out with an empty space where a logo belongs. Nothing in the build
noticed, because nothing in the build was looking.

Serving them ourselves removes that dependency, and three others with it:
the reader's browser no longer calls a third party while reading our
documentation, the site builds without network access, and a rate limit
on someone else's service cannot blank a page.

## What they are

Marks of the vendors whose software or hardware each probe reads,
reproduced to identify that system and for no other purpose. They remain
the property of their respective owners, and their presence here implies
no partnership, endorsement or certification. Sources are icon sets that
redistribute brand marks for identification: Simple Icons, the Iconify
`logos` and `devicon` collections, and Material Design Icons for the
generic ones.

Two are worth knowing about. `exchange_online.svg` carries the Microsoft
corporate mark because no Exchange product mark exists in any of those
sets. `swarm.svg` exists for the catalogue card on the probes index; the
Swarm page itself carries no header logo.

## Replacing one

Drop the new file in with the same name. Keep the class on the `<img>`
as it is: `probe-page-logo-si` and `-wm` are lightened on the dark theme
while `-mdi` is inverted, so a wrong class makes the logo disappear for
half the readers. A monochrome glyph fetched with a colour query must
keep that colour baked in, since nothing recolours it here.
