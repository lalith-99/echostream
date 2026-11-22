FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/server ./cmd/migrate

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/ ./
USER 65532:65532
EXPOSE 8081
CMD ["/app/server"]
