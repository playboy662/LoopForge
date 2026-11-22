# Shared build toolchain. Application services and database data are separate.
FROM node:22-bookworm-slim AS node
FROM maven:3.9.11-eclipse-temurin-21-noble

COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules

RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
       git openssh-client python3 ripgrep ca-certificates curl unzip procps default-mysql-client \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global @openai/codex@0.171.0 \
    && npm cache clean --force \
    && java -version \
    && mvn --version \
    && node --version \
    && npm --version \
    && python3 --version \
    && mysql --version \
    && codex --version

WORKDIR /workspace
