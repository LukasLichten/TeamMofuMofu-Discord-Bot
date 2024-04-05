FROM golang:1.22.2-alpine3.19 AS builder

WORKDIR /build

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /tmm-discord-bot

FROM alpine:3.19

WORKDIR /app 

COPY --from=builder /tmm-discord-bot / 

EXPOSE 2434

ENTRYPOINT [ "/tmm-discord-bot" ]
