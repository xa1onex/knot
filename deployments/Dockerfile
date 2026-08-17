# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/knotd ./cmd/knotd

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/knotd /knotd
EXPOSE 8787
USER nonroot:nonroot
ENTRYPOINT ["/knotd"]
