FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build

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
