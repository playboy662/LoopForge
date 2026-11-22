#!/bin/sh
# Installation may also run in the platform staging workspace. Probe Docker
# only when the installed command is invoked on the real executor.
set -eu

: "${PREFIX:?LoopForge must provide the isolated environment directory}"
mkdir -p "$PREFIX/bin"
printf '%s\n' \
    '#!/bin/sh' \
    'set -eu' \
    'for tool in docker git python3; do command -v "$tool" >/dev/null 2>&1 || { printf "执行机器缺少必需工具：%s\n" "$tool" >&2; exit 1; }; done' \
    'docker version --format "{{.Server.Version}}"' \
    'docker compose version' \
    'git --version' \
    'python3 --version' \
    'printf "Docker Compose 测试交付工具可用；尚未启动应用。\n"' \
    > "$PREFIX/bin/loopforge-docker-check"
chmod 700 "$PREFIX/bin/loopforge-docker-check"
printf '已在隔离 PREFIX 中安装 loopforge-docker-check；由 SOP 在执行机调用。\n'
