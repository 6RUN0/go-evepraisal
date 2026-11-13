FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags='-s -w' -o /out/evepraisal ./evepraisal && \
    mkdir -p /out/data/db/backups

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=build /out/evepraisal /usr/local/bin/evepraisal
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
WORKDIR /data
ENV EVEPRAISAL_DB_PATH=/data/db/ EVEPRAISAL_BACKUP_PATH=/data/db/backups/
VOLUME /data
EXPOSE 8080
HEALTHCHECK --start-period=30m CMD ["/usr/local/bin/evepraisal", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/evepraisal"]
