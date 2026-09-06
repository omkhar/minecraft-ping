FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS build

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
