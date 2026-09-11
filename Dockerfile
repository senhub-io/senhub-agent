# The agent as a container: one static binary, an unprivileged user, and
# a configuration built from the environment on first start.
#
# The image is deliberately thin. It carries the release binary, the
# certificates it needs to reach a collector over TLS, and an entrypoint
# that writes a configuration when none is mounted. Everything else is a
# parameter.
#
#   docker build --build-arg VERSION=0.5.5-beta -t senhub-agent .
#
# VARIANT picks which build of the release is taken: `oss` needs no
# licence and carries the free probes; the default carries the paid ones
# and reads the licence from SENHUB_LICENSE.
ARG ALPINE_VERSION=3.20

FROM alpine:${ALPINE_VERSION} AS fetch
ARG VERSION
ARG VARIANT=""
ARG TARGETARCH=amd64
RUN test -n "$VERSION" || (echo "VERSION build-arg is required" >&2; exit 1)
RUN apk add --no-cache curl unzip
WORKDIR /out
# The release publishes senhub-agent-linux-<arch>.zip and the oss variant
# beside it, each with a detached minisign signature. The signature is
# verified by whoever publishes the image, not here: this stage only
# fetches, so the build stays reproducible from a tag.
RUN set -eu; \
    name="senhub-agent${VARIANT:+-$VARIANT}-linux-${TARGETARCH}.zip"; \
    curl -fsSL -o agent.zip \
      "https://github.com/senhub-io/senhub-agent/releases/download/${VERSION}/${name}"; \
    unzip -q agent.zip; \
    chmod +x senhub-agent

FROM alpine:${ALPINE_VERSION}
ARG VERSION
LABEL org.opencontainers.image.title="SenHub Agent" \
      org.opencontainers.image.description="Infrastructure monitoring agent: probes to PRTG, Nagios, Prometheus, OTLP." \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/senhub-io/senhub-agent" \
      org.opencontainers.image.documentation="https://agent.senhub.io/docs" \
      org.opencontainers.image.vendor="Sensor Factory"

# ca-certificates for TLS to a collector, tzdata so a timestamp is not
# stamped in UTC by accident when the operator sets TZ.
RUN apk add --no-cache ca-certificates tzdata

# The agent needs no privileges to run: only the service-lifecycle
# commands do, and a container never calls them.
RUN addgroup -g 10001 -S senhub && adduser -u 10001 -S -G senhub -h /var/lib/senhub-agent senhub

COPY --from=fetch /out/senhub-agent /usr/local/bin/senhub-agent
COPY packaging/docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# /etc/senhub-agent holds the configuration, /var/lib/senhub-agent what
# must survive a restart, and /var/log/senhub-agent the log file whose
# path the agent does not take from configuration: without it the agent
# falls back to the directory of its own binary, which is read-only here.
RUN mkdir -p /etc/senhub-agent /var/lib/senhub-agent /var/log/senhub-agent \
 && chown -R senhub:senhub /etc/senhub-agent /var/lib/senhub-agent /var/log/senhub-agent
VOLUME ["/var/lib/senhub-agent"]

USER senhub
WORKDIR /var/lib/senhub-agent
EXPOSE 8080

# The console answers on /health once the HTTP output is listening.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${SENHUB_HTTP_PORT:-8080}/health" || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["run"]
