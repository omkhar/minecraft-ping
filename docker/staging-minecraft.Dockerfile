FROM golang:1.27rc1-bookworm@sha256:19163082e6ab06e25286f6900720a5360dbd7ba9791802880fb44c9d4c6c5fe8 AS build

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
