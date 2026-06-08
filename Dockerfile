# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /workspace
COPY go.mod go.mod
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o manager main.go

# Final stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/manager"]
