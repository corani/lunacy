# Lunacy client

Build with `go build -o bin/lunacy-cli ./cmd/lunacy-cli`. Configure the `client`
section shown in `config.yaml.example`: `base_url`, optional Basic Auth
`username`/`password`, positive `timeout` (Go duration, default `30s`), and
`watermark`. Credentials are independent of the server section. Prefer HTTPS;
HTTP sends Basic Auth credentials without encryption. TLS verification is always
enabled and redirects are refused. Deployment URL prefixes are preserved.

```sh
bin/lunacy-cli --config ./config.yaml sync
bin/lunacy-cli sync --config ./config.yaml --output sync.json
bin/lunacy-cli sync --full --output full.json
bin/lunacy-cli sync --since 2026-11-04T04:00:00.000Z
bin/lunacy-cli book --filename '/storage/books/example.epub'
bin/lunacy-cli cover --filename '/storage/books/example.epub' --output cover.png
```

Without `--config`, search `./config.yaml`, then
`$XDG_CONFIG_HOME/lunacy/config.yaml`, then `$HOME/.config/lunacy/config.yaml`.
An existing config is required; symlinks resolve to their exact target.

JSON is pretty-printed to stdout or `--output`; covers require an output file.
Diagnostics go to stderr and failures exit nonzero. JSON content type and response
shape, timestamps, reading metrics, arrays, annotation counts, and annotation/parent
associations are validated before any
output or cursor advancement. Unknown fields and numeric source values survive
unchanged. This is an API inspection tool, not an Obsidian importer.

**Every successful sync ALWAYS saves the returned watermark in the selected
config**, including `--full` and `--since` replays and empty responses. Saving
occurs only after validation and complete successful output. The atomic
same-directory replacement retains YAML comments, unrelated settings and
credentials, and existing permission bits. New output files use mode `0600`;
staging files are cleaned up on failure. A save failure returns nonzero even if
output succeeded; the old cursor remains and the next request replays.
Book and cover commands never change the cursor.

Default sync uses the saved watermark; an empty watermark omits `since` and
retrieves the full eligible library. To replay, use `--full`, override `--since`,
or reset `client.watermark` to `""`. Timestamps require RFC3339 with an explicit
timezone and at most three fractional digits. The server rejects future values.
The lower boundary is **inclusive**: consumers must upsert/deduplicate books by
filename and annotations by generated `id` (the API/callout identity), not append
blindly. `ann_id` is only the source-derived deduplication identity used during
imports. Generated annotation IDs are stable for the retained database's lifetime,
not across database rebuilds; consumers must reconcile/reset their identities
when replacing the database.

`baseline_at` records when sync tracking was established, not historical source
creation time. Initial tracking exposes existing eligible records at baseline.
After restoring an older server database, reset/replay the cursor and reconcile
consumer state with the restored database, including annotation identities.
Run only one client per config at a time; concurrent syncs are not coordinated.
Do not select the config itself as the output file.
