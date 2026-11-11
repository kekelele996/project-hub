FROM golang:1.25-alpine AS build
# Build-network workaround for networks where dl-cdn.alpinelinux.org is unreliable.
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
    && apk add --no-cache git
ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOBIN=/out go install github.com/boyter/scc/v3@v3.7.0

FROM alpine:3.22
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app \
    && mkdir -p /data/uploads && chown app:app /data/uploads
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/scc /usr/local/bin/scc
USER app
ENV UPLOAD_DIR=/data/uploads SCC_BINARY=/usr/local/bin/scc PORT=8080
EXPOSE 8080
CMD ["server"]
