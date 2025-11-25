FROM golang:1.24-alpine AS builder
RUN apk update && apk add make git &&\
    rm -rf /var/cache/apk/*
WORKDIR /app
COPY hack hack
COPY src src
COPY .git .git
COPY Makefile .
RUN make build

FROM scratch
COPY --from=builder /app/zedex /bin/zedex
ENTRYPOINT [ "/bin/zedex" ]
