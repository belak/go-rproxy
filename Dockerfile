# Stage 1: Build the application
FROM golang:1.13-alpine as builder

RUN apk add -U --no-cache build-base git

RUN mkdir /build
RUN mkdir /rproxy
WORKDIR /rproxy

ADD go.mod go.sum ./
RUN go mod download

ADD . .
RUN go build -v -o /build/rproxy .

# Stage 2: Copy files and configure what we need
FROM alpine:latest

# Copy the built seabird into the container
COPY --from=builder /build /bin

EXPOSE 80
EXPOSE 443

VOLUME "/srv/rproxy"

ENTRYPOINT ["/bin/rproxy"]
