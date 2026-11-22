FROM golang as builder

WORKDIR /workspace
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 go build -o kubectl-pwru ./cmd/host/main.go

FROM docker.io/cilium/pwru:v1.0.12
COPY --from=builder /workspace/kubectl-pwru /kubectl-pwru

ENTRYPOINT [ "/kubectl-pwru" ]
