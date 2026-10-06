# Run from the repository root.
# The builder runs on the build platform and cross-compiles, so multi-arch
# builds don't compile under emulation.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS builder
ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /usr/src/kube-ovn-network-binding-plugin
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /kube-ovn-network-binding-sidecar ./sidecar

FROM gcr.io/distroless/static:nonroot AS sidecar
COPY --from=builder /kube-ovn-network-binding-sidecar /kube-ovn-network-binding-sidecar

ENTRYPOINT ["/kube-ovn-network-binding-sidecar"]
