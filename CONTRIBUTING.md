# Contributing

Thanks for considering a contribution to Fidibeki.

## Before you start

Open an issue before beginning a large feature or a change that affects the public API, database schema, privacy behaviour, or deployment configuration. Small fixes and documentation improvements can go straight to a pull request.

Do not include customer data, production URLs, access tokens, screenshots from real environments, prospect lists, or internal planning notes in an issue, pull request, test, or commit.

## Development

Use the prerequisites and local-stack instructions in the [README](README.md). Copy `.env.example` to `backend/.env`; values in that file are local-only examples. Never use production credentials locally or commit an environment file.

Run the focused checks for the area you changed. Before opening a pull request, run the relevant test, type-check, and build commands listed in the workspace `package.json` files. Backend changes should also pass the checks described by the `Makefile`.

## Pull requests

Keep each pull request narrowly scoped. Explain the user-visible effect, tests run, and any migration or configuration changes. Add or update tests for a behaviour change.

By submitting a contribution, you confirm that you have the right to submit it under this repository's license.
