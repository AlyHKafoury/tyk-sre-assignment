FROM golang:1.22 as builder

WORKDIR /app

COPY ./golang/go.mod ./golang/go.sum ./

RUN go mod tidy
RUN go mod download

COPY ./golang/ .
RUN go mod tidy
RUN GOOS=linux GOARCH=amd64 go build -o myapp

FROM alpine:latest

WORKDIR /app/
RUN adduser -D app
COPY --from=builder --chown=app:app /app/myapp .

EXPOSE 8080
USER app

CMD ["./myapp"]