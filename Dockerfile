FROM golang:1.22.8-alpine AS builder

ENV GO111MODULE=on
WORKDIR /app/
COPY . /app/
RUN go mod download
RUN CGO_ENABLED=0 go build cmd/das/main.go

FROM ubuntu

COPY --from=builder /app/main /das
