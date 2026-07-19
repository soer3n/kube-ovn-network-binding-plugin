# Run from the repository root.
FROM docker.io/library/golang:1.26 AS builder

WORKDIR /usr/src/kube-ovn-network-binding-plugin
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /kube-ovn-network-binding-sidecar ./sidecar

FROM gcr.io/distroless/static:nonroot AS sidecar
COPY --from=builder /kube-ovn-network-binding-sidecar /kube-ovn-network-binding-sidecar

ENTRYPOINT ["/kube-ovn-network-binding-sidecar"]
