FROM golang:1.22.1

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /tmm-bot

EXPOSE 2434

ENTRYPOINT [ "/tmm-bot" ]
