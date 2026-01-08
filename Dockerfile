FROM --platform=$BUILDPLATFORM golang:1 AS build

WORKDIR /go/src/github.com/Snawoot/opera-proxy
COPY . .
ARG TARGETOS TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -a -tags netgo -ldflags '-s -w -extldflags "-static"'

FROM scratch
COPY --from=build /go/src/github.com/Snawoot/opera-proxy/opera-proxy /
USER 9999:9999
EXPOSE 18080/tcp
ENTRYPOINT ["/opera-proxy", "-bind-address", "0.0.0.0:18080"]
