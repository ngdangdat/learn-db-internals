FROM mcr.microsoft.com/devcontainers/go:1-1.23-bookworm

USER root

RUN printf '#!/bin/sh\nexec git status "$@"\n' > /usr/local/bin/gst \
    && chmod +x /usr/local/bin/gst

USER vscode

WORKDIR /workspaces/learn-db-internals
