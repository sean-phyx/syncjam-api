FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/syncjam ./cmd/syncjam

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/syncjam /syncjam
ENV PORT=8787
EXPOSE 8787
USER nonroot
ENTRYPOINT ["/syncjam"]
