# Next (unreleased)

Nothing released yet since 0.6.1. Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

- **filetail reports where each tail stands.** For every file followed, `senhub.filetail.read_offset` and `senhub.filetail.file_size` (Prometheus `senhub_filetail_read_offset_bytes` and `senhub_filetail_file_size_bytes`, attribute `log.file.path`) let a rule detect a frozen tail: the file grew and the offset did not move.
