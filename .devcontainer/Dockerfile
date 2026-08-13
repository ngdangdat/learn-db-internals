FROM mcr.microsoft.com/devcontainers/go:1-1.23-bookworm

USER root

# The base image ships a stale Yarn apt source that breaks feature installation.
RUN find /etc/apt/sources.list.d -type f -iname '*yarn*' -delete

USER vscode

WORKDIR /workspaces/learn-db-internals
