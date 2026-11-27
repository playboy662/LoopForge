# Third-Party Notices

## ECC capability assets

`deploy/catalog/ecc/upstream/` contains selected, unmodified skills and role
references from [ECC](https://github.com/affaan-m/ECC), pinned to commit
`3f14a103a8694e51c1df9a806217a6d0b5f7a6f7`.

ECC is MIT-licensed; its original license is retained at
`deploy/catalog/ecc/upstream/LICENSE`. File hashes and source paths are recorded
in `deploy/catalog/ecc/upstream-lock.json`. The LoopForge role definitions and
skill bindings are maintained separately in `deploy/catalog/ecc/catalog.json`.
The individual files retain upstream contributor attributions.

## Supplemental capability assets

The following selected assets are retained with their original MIT licenses,
source paths and SHA-256 manifests in each directory's `upstream-lock.json`:

| Directory | Upstream | Pinned commit |
| --- | --- | --- |
| `deploy/catalog/superpowers/upstream/` | https://github.com/obra/superpowers | `22f0b463e7a5e579904dd6211a520321035526f7` |
| `deploy/catalog/agency-agents/upstream/` | https://github.com/msitarzewski/agency-agents | `8ae6164602c95ca78828b148b9052460ee426885` |
| `deploy/catalog/bmad/upstream/` | https://github.com/bmad-code-org/BMAD-METHOD (V6.12) | `bab9082c3e884e0d72c759bcd1a6d90bcc01be84` |

These upstream files are unmodified. `deploy/catalog/supplemental.json`
defines their LoopForge role and skill bindings. The BMAD product-brief skill
has a separate LoopForge entrypoint at
`deploy/catalog/bmad/skills/bmad-product-brief/SKILL.md`, which maps the original
standalone protocol's Canvas to workspace artifacts. BMAD's trademark notice
is retained in its original `LICENSE`; this integration does not imply
upstream endorsement.

## Superpowers

Portions of the following paths are derived from the Superpowers project:

- `local/.codebuddy/skills/brainstorming/`
- `local/.codebuddy/skills/writing-plans/`
- `local/.codex/skills/superpowers/brainstorming/`
- `local/.cursor/skills/brainstorming/`
- `local/.cursor/skills/writing-plans/`
- `local/.claude/skills/brainstorming/`
- `local/.claude/skills/writing-plans/`

Reviewed upstream version: `v5.1.0`

Upstream project: <https://github.com/obra/superpowers>

The local copies are vendored and may contain compatibility adaptations for this workflow.

MIT License

Copyright (c) 2025 Jesse Vincent

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Platform editor distributions

- `web/monaco/`: Monaco Editor, MIT, Microsoft. The original `LICENSE` and `ThirdPartyNotices.txt` are included.
- `web/vscode-web/`: Code-OSS web distribution from `vscode-web@1.91.1`, based on Microsoft Visual Studio Code. The Code-OSS MIT license and upstream third-party notices are included in that directory. This is not the proprietary Microsoft-branded Visual Studio Code distribution.
- `go.mod` / `go.sum`: Go dependencies; complete original module licenses and notices are bundled in `third_party/go/THIRD_PARTY_NOTICES.txt`. Refresh with `python3 scripts/collect-go-notices.py --write` after reviewing dependency changes. Binary distributions must retain this bundle.

## Executor CLI packages

The Docker build downloads pinned Codex, CodeBuddy and DeepSeek Harness (`@deepseek-ai/dsh`, MIT) CLI packages from their provider registries. DeepSeek Harness depends on `@img/sharp-libvips-linux-x64` / `@img/sharp-libvips-linux-arm64`, unmodified prebuilt libvips shared libraries under LGPL-3.0-or-later; the library versions are listed in the package's `versions.json`, and their sources are available from https://github.com/libvips/libvips and https://github.com/lovell/sharp-libvips. Claude Code is proprietary and is not included in published images. When a conversation asks to install a missing CLI, the platform downloads the pinned `@anthropic-ai/claude-code` or `@deepseek-ai/dsh` package, or the public npm package for OpenCode, Kimi Code or Pi, into the installation's data directory. Cursor CLI and Antigravity are installed with their providers' installers. A LOOP sandbox that selects Claude Code, OpenCode, Kimi Code or Pi installs the same package on first use. These packages and their bundled dependencies retain the licenses/notices shipped by their providers and are subject to those providers' terms; they are not relicensed under LoopForge's MIT license. Provider accounts, credentials and service terms apply separately. The installed packages retain their original license/readme files.

The platform image also includes the official WeCom CLI `@wecom/cli@1.3.4` and its matching platform binary package, distributed under the MIT License, Copyright (c) 2026 WeCom. Their original license and readme files are retained in the image's npm package directories. Source: <https://github.com/WecomTeam/wecom-cli>.

The platform source was contributed by the repository owner from the recorded upstream master and is distributed under the root MIT license.
