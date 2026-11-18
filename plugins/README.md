# plugins/ (developer folder)

Ervisio Console ships no plugins. Plugins come from the marketplace (Plugins › Browse), which lists the public repositories of the
GitHub organisation in `plugins.github_org` (default `Ervisio`). The Docker plugin, which shipped here up to Ervisio 0.3.0,
now lives at https://github.com/Ervisio/plugin-docker.

This folder is scanned as the `dev` location when the daemon runs with `--dev` from the repository root: put a plugin
folder here (for example the `dist/<id>/` output of a plugin built with https://github.com/Ervisio/plugin-sdk), or a
symlink to it, and it is loaded unsigned with an "Unsigned, dev" badge. A `catalog.json` placed here is used as the
local catalog in `--dev`.

Everything in this folder except this file is ignored by git.
