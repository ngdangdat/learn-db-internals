FROM mcr.microsoft.com/devcontainers/go:1-1.23-bookworm

USER root

RUN find /etc/apt/sources.list.d -type f -iname '*yarn*' -delete \
    && apt-get update \
    && apt-get install -y --no-install-recommends zsh \
    && rm -rf /var/lib/apt/lists/*

USER vscode

WORKDIR /workspaces/learn-db-internals
