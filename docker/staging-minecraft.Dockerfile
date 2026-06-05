FROM golang:1.26.4-bookworm@sha256:5d2b868674b57c9e48cdd39e891acce4196b6926ca6d11e9c270a8f85106203d AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -trimpath -ldflags='-s -w' -o /out/minecraft-staging-server ./cmd/staging-server

FROM scratch

COPY --from=build /out/minecraft-staging-server /minecraft-staging-server

EXPOSE 25565/tcp
EXPOSE 19132/udp

ENTRYPOINT ["/minecraft-staging-server", "-listen4", ":25565", "-listen6", "", "-bedrock-listen4", ":19132", "-bedrock-listen6", ""]
