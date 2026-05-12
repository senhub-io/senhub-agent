# Documentation Screenshots

This directory contains screenshots referenced by the Hugo user guide. Any image referenced as `/images/<path>` in a markdown page must exist here (Hugo serves `static/` at the site root).

## Expected files (placeholders referenced in docs)

Drop real screenshots at these paths to replace the placeholder alt-text in the rendered site:

### Homepage
- `dashboard-hero.png` — hero shot of the agent dashboard

### Web interface
- `web-interface/dashboard.png` — main dashboard with probe status, cache size, uptime
- `web-interface/api-explorer.png` — API Explorer with endpoint list and live JSON response
- `web-interface/prtg-sensor-setup.png` — PRTG HTTP Data Advanced sensor creation dialog

### CLI
- `cli/config-check.png` — terminal output of `senhub-agent config check`
- `cli/update-list.png` — terminal output of `senhub-agent update --list`

### Installation
- `installation/windows-service-running.png` — Services.msc with SenHub Agent Running
- `installation/linux-systemd-status.png` — `systemctl status senhub-agent` output

### Configuration
- `configuration/config-reload.png` — agent log showing hot-reload message

## Naming conventions

- Use lowercase, dash-separated filenames: `config-check.png`, not `ConfigCheck.PNG`
- Group by section in subfolders matching the doc tree
- PNG preferred; WebP acceptable for large screenshots
- Max width ~1600px to keep the site lightweight

## Updating a reference

When adding or renaming a screenshot, `grep -rn "images/<old-path>" docs/user-guide/content/` to find and update all references.
