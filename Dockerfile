FROM --platform=$BUILDPLATFORM golang:1.22 AS build

ARG GIT_DESC=undefined

WORKDIR /go/src/github.com/Snawoot/opera-proxy
COPY . .
ARG TARGETOS TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -a -tags netgo -ldflags '-s -w -extldflags "-static" -X main.version='"$GIT_DESC"
ADD https://curl.haxx.se/ca/cacert.pem /certs.crt
RUN chmod 0644 /certs.crt

FROM scratch AS arrange
COPY --from=build /go/src/github.com/Snawoot/opera-proxy/opera-proxy /
COPY --from=build /certs.crt /etc/ssl/certs/ca-certificates.crt

FROM scratch
COPY --from=arrange / /
USER 9999:9999
EXPOSE 18080/tcp
ENTRYPOINT ["/opera-proxy", "-bind-address", "0.0.0.0:18080"]
